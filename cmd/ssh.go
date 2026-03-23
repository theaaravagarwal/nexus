package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"nexus/internal/hosts"
	"nexus/internal/remote"
	"nexus/internal/ui"
)

func newSSHCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh",
		Short: "Open an SSH session from history or a new user@ip",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mustStore()
			if err != nil {
				return err
			}

			knownHosts, err := store.Load()
			if err != nil {
				return fmt.Errorf("failed to load hosts: %w", err)
			}

			host, err := ui.SelectOrQuery("ssh host> ", knownHosts)
			if errors.Is(err, ui.ErrNoSelection) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("host selection failed: %w", err)
			}

			if err := hosts.Validate(host); err != nil {
				return fmt.Errorf("invalid host format %q: %w", host, err)
			}

			if _, err := store.Add(host); err != nil {
				return fmt.Errorf("failed to save host history: %w", err)
			}

			return remote.StartInteractiveSSH(host)
		},
	}
}
