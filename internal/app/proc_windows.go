//go:build windows

package app

import (
	"os"
	"os/exec"
)

func setProcessGroup(*exec.Cmd) {}

func killProcessTree(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}

func checkFileOwnership(string, os.FileInfo) error { return nil }
