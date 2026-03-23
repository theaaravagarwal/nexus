package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"nexus/internal/remote"
	"nexus/internal/transfer"
	"nexus/internal/ui"
)

func newPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Pull a remote file or folder via rsync",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			entries, err := remote.ListPathsForPull(host)
			if err != nil {
				return fmt.Errorf("failed to list remote paths: %w", err)
			}
			if len(entries) == 0 {
				return fmt.Errorf("no remote paths found for %s", host)
			}

			remotePath, err := ui.Select("remote path> ", entries)
			if errors.Is(err, ui.ErrNoSelection) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("remote path selection failed: %w", err)
			}

			if err := transfer.Pull(host, remotePath, "."); err != nil {
				return err
			}

			maybeOpenMedia(remotePath)
			return nil
		},
	}
}

func maybeOpenMedia(remotePath string) {
	if runtime.GOOS != "darwin" {
		return
	}

	ext := strings.ToLower(filepath.Ext(remotePath))
	switch ext {
	case ".mp4", ".mov", ".png", ".jpg":
	default:
		return
	}

	localName := filepath.Base(remotePath)
	if localName == "." || localName == "/" || localName == "" {
		return
	}

	openCmd := exec.Command("open", localName)
	_ = openCmd.Start()
}
