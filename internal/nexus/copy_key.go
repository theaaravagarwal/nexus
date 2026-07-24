package nexus

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// buildSSHCopyIDArgs returns arguments for ssh-copy-id without passing the
// destination through a shell. ssh-copy-id accepts the SSH port separately,
// just like ssh, so saved non-default ports remain intact.
func buildSSHCopyIDArgs(target string) ([]string, error) {
	spec, err := parseConnectionTarget(target)
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, 3)
	if spec.Port != defaultSSHPort {
		args = append(args, "-p", strconv.Itoa(spec.Port))
	}
	return append(args, spec.sshDestination()), nil
}

func runSSHCopyID(target string) error {
	binary, err := exec.LookPath("ssh-copy-id")
	if err != nil {
		return fmt.Errorf("ssh-copy-id not found in PATH: %w", err)
	}
	args, err := buildSSHCopyIDArgs(target)
	if err != nil {
		return err
	}

	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-copy-id failed: %w", err)
	}
	return nil
}
