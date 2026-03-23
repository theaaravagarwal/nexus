package transfer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"nexus/internal/pathutil"
)

func Pull(host, remotePath, localDestination string) error {
	source := fmt.Sprintf("%s:%s", host, remotePath)
	destination, err := pathutil.ExpandUser(localDestination)
	if err != nil {
		return fmt.Errorf("failed to resolve destination path: %w", err)
	}

	return runRsync(source, destination)
}

func Push(localPath, host, remoteDir string) error {
	localPath = pathutil.NormalizeForRsync(filepath.Clean(localPath))
	dest := fmt.Sprintf("%s:%s", host, ensureTrailingSlash(remoteDir))
	return runRsync(localPath, dest)
}

func runRsync(source, destination string) error {
	if _, err := exec.LookPath("rsync"); err != nil {
		return fmt.Errorf("rsync not found in PATH: %w", err)
	}

	cmd := exec.Command("rsync", "-avzP", "--protect-args", source, destination)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync failed: %w", err)
	}
	return nil
}

func ensureTrailingSlash(dir string) string {
	if strings.HasSuffix(dir, "/") {
		return dir
	}
	return dir + "/"
}
