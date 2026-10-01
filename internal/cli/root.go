// Package cli is emguio's command line: the daemon, and what an operator needs a shell for.
//
// The first way in is here and nowhere else: a user exists only because somebody with a shell
// on the host printed an invitation.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"emguio/internal/app"
	"emguio/internal/config"
	"emguio/internal/store"
)

func root() *cobra.Command {
	cmd := &cobra.Command{
		Use:           app.Name,
		Short:         "A webmail client over the mail servers its users name",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Cobra prints to stderr unless an output is set, which would make `link=$(emguio invite)`
	// capture nothing.
	cmd.SetOut(os.Stdout)
	cmd.AddCommand(serveCmd(), inviteCmd(), healthcheckCmd(), versionCmd())
	return cmd
}

// Execute runs the command line and returns a process exit code.
func Execute() int {
	if err := root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, app.Name+":", err)
		return 1
	}
	return 0
}

// setup is the environment, a logger, and the database.
//
// A LevelVar rather than a fixed level, so raising it at runtime stays a one-line change.
func setup() (*config.Config, *store.Store, *slog.Logger, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, err
	}
	level := new(slog.LevelVar)
	level.Set(cfg.LogLevel)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, st, log, nil
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version this binary was built from",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println(app.Version)
			return nil
		},
	}
}
