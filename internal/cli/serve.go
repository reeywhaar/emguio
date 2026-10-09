package cli

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"emguio/internal/api"
	"emguio/internal/app"
	"emguio/internal/config"
	"emguio/internal/connect"
	"emguio/internal/mirror"
	"emguio/internal/store"
	"emguio/internal/sweep"
)

// firstRun prints an invitation when nobody can sign in yet.
//
// No default password exists at any point, so there is no credential for somebody to forget to
// change.
func firstRun(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) error {
	has, err := st.HasUsers(ctx)
	if err != nil || has {
		return err
	}
	inv, token, err := st.CreateInvite(ctx)
	if err != nil {
		return err
	}
	log.Info("no users yet: open this link to make the first one",
		"link", cfg.Link("/invite/"+token), "expires_at", inv.ExpiresAt.Format(time.RFC3339))
	return nil
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, st, log, err := setup()
			if err != nil {
				return err
			}
			defer st.Close()

			// Said at startup so "it works locally" does not become how it is deployed.
			if cfg.PublicURL.Scheme != "https" {
				log.Warn("the public url is http, so the session cookie goes without Secure",
					"url", cfg.PublicURL.String())
			}

			// Read once. A missing directory is the placeholder page, and web/dist is where a
			// checkout's own build lands.
			webDir := app.WebDir
			if _, err := os.Stat(webDir); err != nil {
				webDir = "web/dist"
			}
			var webFS fs.FS
			if _, err := os.Stat(webDir); err == nil {
				webFS = os.DirFS(webDir)
			} else {
				log.Warn("no web directory: browsers get the placeholder page instead of the app",
					"dir", app.WebDir)
			}
			spa, err := api.NewSPA(webFS)
			if err != nil {
				return err
			}
			// The bundle carries the API reference rendered; the source is the fallback for a
			// checkout with no bundle.
			var sourceFS fs.FS
			if _, err := os.Stat(app.DocsDir); err == nil {
				sourceFS = os.DirFS(app.DocsDir)
			} else if _, err := os.Stat("docs"); err == nil {
				sourceFS = os.DirFS("docs")
			}
			docs := api.NewDocs(webFS, sourceFS, cfg.PublicURL.String())

			if err := firstRun(cmd.Context(), cfg, st, log); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Every request is built on this one, so cancelling it ends the event streams that are
			// waiting for something to happen. Shutdown waits for what is in flight, and a stream
			// is in flight until the browser closes the tab.
			streams, endStreams := context.WithCancel(context.Background())
			defer endStreams()

			reader := mirror.New(st, connect.New(cfg.AllowNetworks), log)
			srv := &http.Server{
				Addr:              app.ListenAddr,
				Handler:           api.New(cfg, log, st, spa, docs, reader),
				ReadHeaderTimeout: 10 * time.Second,
				BaseContext:       func(net.Listener) context.Context { return streams },
			}

			// Waited on below, so neither is still writing when the database closes.
			var background sync.WaitGroup
			background.Go(func() { sweep.Run(ctx, st, log) })
			background.Go(func() { reader.Run(ctx) })

			errc := make(chan error, 1)
			go func() {
				log.Info("emguio started", "addr", app.ListenAddr, "version", app.Version,
					"url", cfg.PublicURL.String(), "data", cfg.DataDir)
				errc <- srv.ListenAndServe()
			}()

			select {
			case err := <-errc:
				stop()
				background.Wait()
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return err
			case <-ctx.Done():
				// Bounded, or one slow request costs the full kill timeout every deploy.
				shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				log.Info("emguio stopping")
				// Before Shutdown rather than after: it waits for active requests, and a stream
				// only stops being active when its context ends.
				endStreams()
				err := srv.Shutdown(shutdown)
				// Before the deferred Close, or a sweep mid-pass writes into a database that is
				// being checkpointed.
				background.Wait()
				return err
			}
		},
	}
}
