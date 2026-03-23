package remote

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const connectTimeoutSeconds = "5"

func StartInteractiveSSH(host string) error {
	args := append(commonSSHArgs(false), host)

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ListPathsForPull(host string) ([]string, error) {
	const remoteFind = "find ~ -maxdepth 3 \\( -path '*/.git' -o -path '*/.git/*' \\) -prune -o -mindepth 1 -print"
	out, err := runSSHCommand(host, remoteFind)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func ListDirectoriesForPush(host string) ([]string, error) {
	const remoteFind = "find ~ -path '*/.git' -prune -o -type d -print"
	out, err := runSSHCommand(host, remoteFind)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func runSSHCommand(host, remoteCmd string) (string, error) {
	args := append(commonSSHArgs(true), host, remoteCmd)
	cmd := exec.Command("ssh", args...)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		stderr := strings.TrimSpace(errOut.String())
		if stderr == "" {
			stderr = "host is unreachable or ssh command failed"
		}
		return "", fmt.Errorf("%w: %s", err, stderr)
	}
	return out.String(), nil
}

func commonSSHArgs(batchMode bool) []string {
	args := []string{
		"-o", "ConnectTimeout=" + connectTimeoutSeconds,
		"-o", "LogLevel=ERROR",
		"-q",
	}
	if batchMode {
		args = append(args, "-o", "BatchMode=yes", "-T")
	}
	return args
}

func splitLines(input string) []string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
