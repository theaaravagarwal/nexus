//go:build unix

package app

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup runs the command in its own process group so a hard kill
// can take the whole tree (ssh, remote shell wrapper) with it.
func setProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree kills the command's process group, falling back to the
// direct child if the group kill is refused.
func killProcessTree(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil {
		_ = command.Process.Kill()
	}
}

// checkFileOwnership returns an error when the file is not owned by the
// current user, so a config planted by another local account is refused.
func checkFileOwnership(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("config file is not owned by current user: %s", path)
	}
	return nil
}
