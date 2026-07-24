package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var errNoRemoteProbeCandidates = errors.New("no remote probe candidates available")

var (
	monitorProbeCandidates = []string{"btop", "btm", "bashtop", "htop", "top"}
	networkProbeCandidates = []string{
		"iftop", "nload", "iptraf-ng", "speedtest-cli", "speedtest",
		"ip -s link", "ifconfig", "netstat -i",
	}
	storageProbeCandidates = []string{"duf", "dust", "ncdu", "df -h"}
)

func (a *app) newTopCmd() *cobra.Command {
	return a.newDiagnosticCommand(
		"top [user@host[:port]]",
		[]string{"btop"},
		"Open a remote system monitor",
		monitorProbeCandidates,
		false,
	)
}

func (a *app) newNetCmd() *cobra.Command {
	command := a.newDiagnosticCommand(
		"net [user@host[:port]]",
		[]string{"network", "bandwidth"},
		"Inspect remote network throughput",
		networkProbeCandidates,
		true,
	)
	command.RunE = func(cmd *cobra.Command, args []string) error {
		host, err := a.resolveDiagnosticHost(args)
		if errors.Is(err, errCancelled) {
			return nil
		}
		if err != nil {
			return err
		}
		err = runRemoteProbe(host, networkProbeCandidates, true)
		if errors.Is(err, errNoRemoteProbeCandidates) {
			fmt.Printf("No network utility found on %s; using portable interface counters\n", host)
			summary := `printf '%s\n' 'Network interfaces'; ` +
				`if [ -r /proc/net/dev ]; then cat /proc/net/dev; ` +
				`else printf '%s\n' 'No portable interface counters are exposed by this system.'; fi`
			err = runRemoteInteractiveCommand(host, summary)
		}
		if err != nil {
			return fmt.Errorf("net failed: %w", err)
		}
		if err := a.recordSuccess(host); err != nil {
			logVerbose("failed to record host activity: %v", err)
		}
		return nil
	}
	return command
}

func (a *app) newInfoCmd() *cobra.Command {
	command := a.newDiagnosticCommand(
		"info [user@host[:port]]",
		[]string{"neofetch", "fetch", "specs"},
		"Show remote hardware and OS summary",
		[]string{"fastfetch", "neofetch --off", "screenfetch"},
		false,
	)
	command.RunE = func(cmd *cobra.Command, args []string) error {
		host, err := a.resolveDiagnosticHost(args)
		if errors.Is(err, errCancelled) {
			return nil
		}
		if err != nil {
			return err
		}
		err = runRemoteProbe(host, []string{"fastfetch", "neofetch --off", "screenfetch"}, false)
		if err == nil {
			if refreshErr := refreshHostMetadata(a.stateFile, host); refreshErr != nil {
				logVerbose("metadata cache refresh failed: %v", refreshErr)
			}
			if recordErr := a.recordSuccess(host); recordErr != nil {
				logVerbose("failed to record host activity: %v", recordErr)
			}
			return nil
		}
		if !errors.Is(err, errNoRemoteProbeCandidates) {
			return fmt.Errorf("info probe failed: %w", err)
		}

		fmt.Printf("No fetch utility found on %s; using the portable summary\n", host)
		summary := `printf '%s\n' 'System'; (uname -srm 2>/dev/null || ver 2>/dev/null || true); ` +
			`if [ -r /etc/os-release ]; then sed -n 's/^PRETTY_NAME=//p' /etc/os-release | tr -d '"'; fi; ` +
			`printf '\n%s\n' 'CPU / memory'; (command -v lscpu >/dev/null 2>&1 && lscpu | sed -n 's/^Model name:[[:space:]]*//p') || ` +
			`(command -v sysctl >/dev/null 2>&1 && sysctl -n machdep.cpu.brand_string 2>/dev/null) || true; ` +
			`(command -v free >/dev/null 2>&1 && free -h) || (command -v vm_stat >/dev/null 2>&1 && vm_stat) || true`
		if err := runRemoteInteractiveCommand(host, summary); err != nil {
			return err
		}
		if refreshErr := refreshHostMetadata(a.stateFile, host); refreshErr != nil {
			logVerbose("metadata cache refresh failed: %v", refreshErr)
		}
		if recordErr := a.recordSuccess(host); recordErr != nil {
			logVerbose("failed to record host activity: %v", recordErr)
		}
		return nil
	}
	return command
}

func (a *app) newStorageCmd() *cobra.Command {
	return a.newDiagnosticCommand(
		"storage [user@host[:port]]",
		[]string{"disk", "du", "io"},
		"Inspect remote disk usage and I/O health",
		storageProbeCandidates,
		false,
	)
}

func (a *app) newDiagnosticCommand(use string, aliases []string, short string, candidates []string, sudo bool) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := a.resolveDiagnosticHost(args)
			if errors.Is(err, errCancelled) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := runRemoteProbe(host, candidates, sudo); err != nil {
				return fmt.Errorf("%s failed: %w", strings.Fields(use)[0], err)
			}
			if err := a.recordSuccess(host); err != nil {
				logVerbose("failed to record host activity: %v", err)
			}
			return nil
		},
	}
}

func (a *app) resolveDiagnosticHost(args []string) (string, error) {
	hostArg := ""
	if len(args) > 0 {
		hostArg = strings.TrimSpace(args[0])
	}
	host, err := a.resolveHostForTransfer(hostArg)
	if errors.Is(err, errCancelled) {
		fmt.Println("No host selected.")
		return "", errCancelled
	}
	return host, err
}

func runRemoteProbe(host string, fallbacks []string, sudo bool) error {
	commands, err := resolveRemoteProbeCommands(host, fallbacks)
	if err != nil {
		return err
	}

	sudoReady := false
	if sudo {
		sudoReady, err = remoteSupportsPasswordlessSudo(host)
		if err != nil {
			return err
		}
	}

	var lastErr error
	for _, command := range commands {
		if sudo && requiresElevatedNetworkAccess(command) && !sudoReady {
			continue
		}
		fmt.Printf("Using %s on %s\n", command, host)
		runScript := "exec " + command
		if sudo && requiresElevatedNetworkAccess(command) {
			runScript = `if [ "$(id -u 2>/dev/null || echo 1)" -eq 0 ]; then exec ` + command +
				`; fi; exec sudo -n ` + command
		}
		lastErr = runRemoteInteractiveCommand(host, runScript)
		if lastErr == nil {
			return nil
		}
		// Exit 1 is often a real result from a tool that did run. Retrying it
		// through more shells can repeat side effects and hide the first error.
		if !shouldRetryRemoteShell(lastErr) {
			return lastErr
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return errNoRemoteProbeCandidates
}

func resolveRemoteProbeCommands(host string, fallbacks []string) ([]string, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, fmt.Errorf("ssh not found in PATH: %w", err)
	}
	probeScript, commandByToken := buildRemoteProbeScript(fallbacks)
	if probeScript == "" {
		return nil, errNoRemoteProbeCandidates
	}

	cmd, err := buildSSHCommand(context.Background(), host, false, remoteShellCommand("sh", probeScript))
	if err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// POSIX shells are allowed to return either 1 or 127 when every
		// command -v lookup misses. Both mean "use the portable fallback,"
		// not that the SSH connection or probe failed.
		if isRemoteProbeMiss(err) {
			return nil, errNoRemoteProbeCandidates
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("remote command probe failed: %w: %s", err, message)
		}
		return nil, fmt.Errorf("remote command probe failed: %w", err)
	}

	var ordered []string
	seen := map[string]struct{}{}
	for _, line := range strings.Split(strings.ReplaceAll(stdout.String(), "\r\n", "\n"), "\n") {
		token := strings.TrimSpace(line)
		command, ok := commandByToken[token]
		if !ok {
			continue
		}
		if _, exists := seen[command]; exists {
			continue
		}
		seen[command] = struct{}{}
		ordered = append(ordered, command)
	}
	if len(ordered) == 0 {
		return nil, errNoRemoteProbeCandidates
	}
	return ordered, nil
}

func isRemoteProbeMiss(err error) bool {
	return isExitCode(err, 1) || isExitCode(err, 127)
}

func buildRemoteProbeScript(fallbacks []string) (string, map[string]string) {
	commandByToken := make(map[string]string)
	var tokens []string
	for _, fallback := range fallbacks {
		command := strings.TrimSpace(fallback)
		token := firstToken(command)
		if token == "" {
			continue
		}
		if _, exists := commandByToken[token]; exists {
			continue
		}
		commandByToken[token] = command
		tokens = append(tokens, shellQuote(token))
	}
	if len(tokens) == 0 {
		return "", commandByToken
	}
	return `for cmd in ` + strings.Join(tokens, " ") +
		`; do command -v "$cmd" >/dev/null 2>&1 && printf '%s\n' "$cmd"; done`, commandByToken
}

func remoteSupportsPasswordlessSudo(host string) (bool, error) {
	script := `if [ "$(id -u 2>/dev/null || echo 1)" -eq 0 ]; then exit 0; fi; command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1`
	cmd, err := buildSSHCommand(context.Background(), host, false, remoteShellCommand("sh", script))
	if err != nil {
		return false, err
	}
	err = cmd.Run()
	if err == nil {
		return true, nil
	}
	if isExitCode(err, 1) {
		return false, nil
	}
	return false, err
}

func requiresElevatedNetworkAccess(command string) bool {
	switch firstToken(command) {
	case "iftop", "nload", "iptraf-ng":
		return true
	default:
		return false
	}
}

func runRemoteInteractiveCommand(host, runScript string) error {
	runners := []string{
		runScript,
		remoteShellCommand("sh", runScript),
		remoteShellCommand("bash", runScript),
		remoteShellCommand("zsh", runScript),
	}
	var lastErr error
	for _, remoteCommand := range runners {
		cmd, err := buildSSHCommand(context.Background(), host, true, remoteCommand)
		if err != nil {
			return err
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		lastErr = cmd.Run()
		if lastErr == nil {
			return nil
		}
		if !isExitCode(lastErr, 1) && !isExitCode(lastErr, 126) && !isExitCode(lastErr, 127) {
			return lastErr
		}
	}
	return fmt.Errorf("unable to run remote command on %s: %w", host, lastErr)
}

func shouldRetryRemoteShell(err error) bool {
	return isExitCode(err, 126) || isExitCode(err, 127)
}

func remoteShellCommand(shell, script string) string {
	return shell + " -c " + shellQuote(script)
}

func firstToken(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
