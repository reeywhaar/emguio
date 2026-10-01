package cli

import (
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"emguio/internal/app"
)

// inviteCmd is how a user starts. The token is readable exactly once, so a lost link is
// reissued rather than recovered.
func inviteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "invite",
		Short: "Print a new invitation link",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, st, _, err := setup()
			if err != nil {
				return err
			}
			defer st.Close()

			inv, token, err := st.CreateInvite(cmd.Context())
			if err != nil {
				return err
			}
			cmd.Println(cfg.Link("/invite/" + token))
			cmd.PrintErrf("expires %s\n", inv.ExpiresAt.Format(time.RFC3339))
			return nil
		},
	}
}

// healthcheckCmd is what HEALTHCHECK runs: a second process asking the first, so the image
// needs no HTTP client and a wedged server fails it.
func healthcheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "healthcheck",
		Short: "Ask the running server whether it is well",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get("http://127.0.0.1" + app.ListenAddr + "/healthz")
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("healthz answered %s", resp.Status)
			}
			return nil
		},
	}
}
