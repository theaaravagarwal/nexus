package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"nexus/internal/pathutil"
	"nexus/internal/remote"
	"nexus/internal/transfer"
	"nexus/internal/ui"
)

func newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push [file]",
		Short: "Push a local file or directory to a remote directory via rsync",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			localPath, err := pathutil.ExpandUser(args[0])
			if err != nil {
				return fmt.Errorf("failed to resolve local path: %w", err)
			}

			if _, err := os.Stat(localPath); err != nil {
				return fmt.Errorf("local path does not exist: %w", err)
			}

			store, err := mustStore()
			if err != nil {
				return err
			}

			host, err := selectKnownHost(store)
			if errors.Is(err, ui.ErrNoSelection) {
				return nil
			}
			if err != nil {
				return err
			}

			directories, err := remote.ListDirectoriesForPush(host)
			if err != nil {
				return fmt.Errorf("failed to list remote directories: %w", err)
			}
			if len(directories) == 0 {
				return fmt.Errorf("no remote directories found for %s", host)
			}

			remoteDir, err := ui.Select("remote dir> ", directories)
			if errors.Is(err, ui.ErrNoSelection) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("remote directory selection failed: %w", err)
			}

			return transfer.Push(localPath, host, remoteDir)
		},
	}
}
