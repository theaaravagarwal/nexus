package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const connectTimeoutSeconds = "5"
const defaultFullIndexDepth = 5

var (
	errCancelled       = errors.New("selection cancelled")
	errHostUnreachable = errors.New("host unreachable")

	userPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	hostPattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*)$`)
	ansiCSI     = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

	hostDiscoveryProfiles = map[string]discoveryProfile{}
	verboseLogging        bool
	fullIndexDepth        = defaultFullIndexDepth
)

type discoveryProfile struct {
	UseUnixDiscovery bool `yaml:"use_unix_discovery"`
	RsyncStability   bool `yaml:"rsync_stability"`
}

type app struct {
	configDir   string
	configFile  string
	hostsFile   string
	verbose     bool
	remoteIndex string
}

func newApp() (*app, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve home directory: %w", err)
	}

	configDir := filepath.Join(home, ".config", "nexus")
	return &app{
		configDir:  configDir,
		configFile: filepath.Join(configDir, "config.yaml"),
		hostsFile:  filepath.Join(configDir, "hosts.json"),
	}, nil
}

func main() {
	a, err := newApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := a.newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (a *app) newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "nexus",
		Short:         "History-first SSH and transfer CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return a.ensureBootstrap()
		},
	}
	root.PersistentFlags().BoolVarP(&a.verbose, "verbose", "v", false, "Enable verbose debug logs")
	root.PersistentFlags().StringVarP(&a.remoteIndex, "indexing", "i", "lazy", "Indexing mode: lazy or full")

	root.AddCommand(a.newSSHCmd())
	root.AddCommand(a.newPullCmd())
	root.AddCommand(a.newPushCmd())
	root.AddCommand(a.newHostCmd())
	root.AddCommand(a.newConfigCmd())

	return root
}

func (a *app) ensureBootstrap() error {
	if a == nil {
		return errors.New("internal error: app is nil")
	}

	if err := os.MkdirAll(a.configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", a.configDir, err)
	}

	info, err := os.Stat(a.hostsFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to check hosts file %s: %w", a.hostsFile, err)
		}
		if err := os.WriteFile(a.hostsFile, []byte("[]\n"), 0o644); err != nil {
			return err
		}
	} else if info.IsDir() {
		return fmt.Errorf("hosts file path is a directory: %s", a.hostsFile)
	} else if info.Size() == 0 {
		if err := os.WriteFile(a.hostsFile, []byte("[]\n"), 0o644); err != nil {
			return err
		}
	}

	if err := ensureConfigFile(a.configFile); err != nil {
		return err
	}
	verboseLogging = a.verbose
	profiles, cfgFullDepth, err := loadConfigFromYAML(a.configFile)
	if err != nil {
		return err
	}
	hostDiscoveryProfiles = profiles
	fullIndexDepth = cfgFullDepth

	return nil
}

func (a *app) readHosts() ([]string, error) {
	if err := a.ensureBootstrap(); err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(a.hostsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read hosts file: %w", err)
	}

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return []string{}, nil
	}

	var hosts []string
	if err := json.Unmarshal(raw, &hosts); err == nil {
		return dedupeKeepOrder(hosts), nil
	}

	var legacy struct {
		Hosts []string `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return nil, fmt.Errorf("invalid hosts.json format: %w", err)
	}
	return dedupeKeepOrder(legacy.Hosts), nil
}

func (a *app) writeHosts(hosts []string) error {
	if err := a.ensureBootstrap(); err != nil {
		return err
	}

	safe := dedupeKeepOrder(hosts)
	encoded, err := json.MarshalIndent(safe, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode hosts file: %w", err)
	}
	encoded = append(encoded, '\n')

	if err := os.WriteFile(a.hostsFile, encoded, 0o644); err != nil {
		return fmt.Errorf("failed to write hosts file: %w", err)
	}
	return nil
}

func (a *app) newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Open ~/.config/nexus/config.yaml in your editor",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureConfigFile(a.configFile); err != nil {
				return err
			}

			editorCmd, editorArgs, err := resolveEditorCommand()
			if err != nil {
				return err
			}
			proc := exec.Command(editorCmd, append(editorArgs, a.configFile)...)
			proc.Stdin = os.Stdin
			proc.Stdout = os.Stdout
			proc.Stderr = os.Stderr
			return proc.Run()
		},
	}
}

func resolveEditorCommand() (string, []string, error) {
	candidates := []string{
		os.Getenv("VISUAL"),
		os.Getenv("EDITOR"),
	}
	if zshVisual, zshEditor := loadEditorFromZshrc(); zshVisual != "" || zshEditor != "" {
		candidates = append(candidates, zshVisual, zshEditor)
	}
	candidates = append(candidates, "nvim", "vim", "nano")

	for _, candidate := range candidates {
		name, args := parseEditorCandidate(candidate)
		if name == "" {
			continue
		}
		if _, err := exec.LookPath(name); err == nil {
			return name, args, nil
		}
	}
	return "", nil, errors.New("no suitable editor found (checked VISUAL/EDITOR, ~/.zshrc, nvim, vim, nano)")
}

func parseEditorCandidate(raw string) (string, []string) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

func loadEditorFromZshrc() (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		return "", ""
	}

	var visual string
	var editor string
	lines := strings.Split(string(raw), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if v, ok := parseShellAssignment(trimmed, "VISUAL"); ok {
			visual = v
		}
		if v, ok := parseShellAssignment(trimmed, "EDITOR"); ok {
			editor = v
		}
	}
	return visual, editor
}

func parseShellAssignment(line, key string) (string, bool) {
	line = strings.TrimSpace(line)
	exportPrefix := "export " + key + "="
	plainPrefix := key + "="

	var value string
	switch {
	case strings.HasPrefix(line, exportPrefix):
		value = strings.TrimSpace(strings.TrimPrefix(line, exportPrefix))
	case strings.HasPrefix(line, plainPrefix):
		value = strings.TrimSpace(strings.TrimPrefix(line, plainPrefix))
	default:
		return "", false
	}

	if idx := strings.Index(value, "#"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	value = strings.Trim(value, `"'`)
	return strings.TrimSpace(value), value != ""
}

func ensureConfigFile(configPath string) error {
	if configPath == "" {
		return errors.New("config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	info, err := os.Stat(configPath)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("config path is a directory: %s", configPath)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to inspect config file: %w", err)
	}
	return os.WriteFile(configPath, []byte(defaultConfigYAML()), 0o644)
}

func defaultConfigYAML() string {
	return "# NEXUS settings\n" +
		"# Maximum recursion depth when --indexing full is used.\n" +
		"full_index_depth: 5\n\n" +
		"# Optional per-host overrides.\n" +
		"# Keys must match the host part of your saved user@host entries.\n" +
		"# Example: if you add \"alice@server.local\", use \"server.local\" as the key.\n" +
		"host_profiles:\n" +
		"  <host-or-ip>:\n" +
		"    # Force Unix command style on remote discovery for this host.\n" +
		"    use_unix_discovery: true\n" +
		"    # Use conservative rsync args for flaky/mixed environments.\n" +
		"    rsync_stability: true\n"
}

func loadConfigFromYAML(configPath string) (map[string]discoveryProfile, int, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, defaultFullIndexDepth, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var parsed struct {
		HostProfiles   map[string]discoveryProfile `yaml:"host_profiles"`
		FullIndexDepth int                         `yaml:"full_index_depth"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, defaultFullIndexDepth, fmt.Errorf("invalid config YAML %s: %w", configPath, err)
	}

	profiles := make(map[string]discoveryProfile, len(parsed.HostProfiles))
	for host, profile := range parsed.HostProfiles {
		key := strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
		if key == "" {
			continue
		}
		profiles[key] = profile
	}
	return profiles, sanitizeFullIndexDepth(parsed.FullIndexDepth), nil
}

func sanitizeFullIndexDepth(raw int) int {
	if raw <= 0 {
		return defaultFullIndexDepth
	}
	return raw
}

func (a *app) appendHostIfNew(host string) (bool, error) {
	if err := validateUserHost(host); err != nil {
		return false, err
	}

	hosts, err := a.readHosts()
	if err != nil {
		return false, err
	}
	for _, h := range hosts {
		if h == host {
			return false, nil
		}
	}

	hosts = append(hosts, host)
	return true, a.writeHosts(hosts)
}

func (a *app) newSSHCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh",
		Short: "Open SSH session from history or new user@ip",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				host string
				err  error
			)

			if len(args) > 0 {
				host = strings.TrimSpace(args[0])
			} else {
				host, err = a.chooseHostAllowNew()
				if errors.Is(err, errCancelled) {
					fmt.Println("No host selected.")
					return nil
				}
				if err != nil {
					return fmt.Errorf("host selection failed: %w", err)
				}
			}

			if err := validateUserHost(host); err != nil {
				return fmt.Errorf("invalid host %q: %w", host, err)
			}
			if _, err := a.appendHostIfNew(host); err != nil {
				return fmt.Errorf("failed to save host history: %w", err)
			}
			fmt.Printf("Connecting to %s\n", host)

			if err := runInteractiveSSH(host); err != nil {
				return fmt.Errorf("Connection Failed: %w", err)
			}
			return nil
		},
	}
}

func (a *app) newPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull [user@ip] [remote-path] [local-dir]",
		Short: "Pull remote file/folder with rsync",
		Args:  cobra.MaximumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				hostArg         string
				remoteSourceArg string
				localDestArg    string
			)
			for _, raw := range args {
				candidate := strings.TrimSpace(raw)
				if candidate == "" {
					continue
				}
				if hostArg == "" && looksLikeUserHost(candidate) {
					hostArg = candidate
					continue
				}
				if remoteSourceArg == "" {
					remoteSourceArg = candidate
					continue
				}
				if localDestArg == "" {
					localDestArg = candidate
				}
			}

			host, err := a.resolveHostForTransfer(hostArg)
			if errors.Is(err, errCancelled) {
				fmt.Println("No host selected.")
				return nil
			}
			if err != nil {
				return err
			}

			indexMode := normalizeRemoteIndexMode(a.remoteIndex)
			remoteSource := strings.TrimSpace(remoteSourceArg)
			remoteIsWindows := false
			if remoteSource == "" {
				remoteSource, remoteIsWindows, err = Maps(host, ".", indexMode, "pull", true)
				if errors.Is(err, errCancelled) {
					return nil
				}
				if err != nil {
					return friendlyDiscoveryError(host, err)
				}
			}
			if !remoteIsWindows {
				remoteIsWindows = detectWindowsTarget(host, remoteSource)
			}
			profile := profileForHost(host)
			stabilityProfile := profile.RsyncStability
			if remoteIsWindows {
				remoteSource = ensureWindowsRemotePath(host, remoteSource)
			}

			localDest := strings.TrimSpace(localDestArg)
			if localDest == "" {
				localStart, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to resolve home directory: %w", err)
				}
				localDest, _, err = Maps("", localStart, indexMode, "pull", false)
				if errors.Is(err, errCancelled) {
					return nil
				}
				if err != nil {
					return err
				}
			}
			localDest = filepath.Clean(localDest)
			localDest = ensureTrailingSlashForMode(localDest, false)
			localDest = normalizeLocalPathForRsync(localDest)

			source := formatRemoteEndpoint(host, remoteSource, remoteIsWindows)
			if err := runRsync(source, localDest, rsyncOptions{
				forceRemoteRsyncPath: remoteIsWindows || stabilityProfile,
				stabilityProfile:     stabilityProfile,
			}); err != nil {
				return err
			}

			maybeOpenMedia(remoteSource)
			return nil
		},
	}
}

func (a *app) newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push [file] [user@ip] [remote-dir]",
		Short: "Push local file/folder to remote directory with rsync",
		Args:  cobra.MaximumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				localPathArg string
				hostArg      string
				remoteDirArg string
			)
			for _, raw := range args {
				candidate := strings.TrimSpace(raw)
				if candidate == "" {
					continue
				}
				if hostArg == "" && looksLikeUserHost(candidate) {
					hostArg = candidate
					continue
				}
				if localPathArg == "" {
					localPathArg = candidate
					continue
				}
				if remoteDirArg == "" {
					remoteDirArg = candidate
				}
			}

			localPath := strings.TrimSpace(localPathArg)
			var err error
			indexMode := normalizeRemoteIndexMode(a.remoteIndex)
			if localPath == "" {
				localStart, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to resolve home directory: %w", err)
				}
				localPath, _, err = Maps("", localStart, indexMode, "push", false)
				if errors.Is(err, errCancelled) {
					return nil
				}
				if err != nil {
					return err
				}
			}

			localPath, err = expandUserPath(localPath)
			if err != nil {
				return fmt.Errorf("failed to resolve local path: %w", err)
			}
			localPath = filepath.Clean(localPath)
			if _, err := os.Stat(localPath); err != nil {
				return fmt.Errorf("local path does not exist: %w", err)
			}

			host, err := a.resolveHostForTransfer(hostArg)
			if errors.Is(err, errCancelled) {
				fmt.Println("No host selected.")
				return nil
			}
			if err != nil {
				return err
			}

			remoteDir := strings.TrimSpace(remoteDirArg)
			remoteIsWindows := false
			if remoteDir == "" {
				remoteDir, remoteIsWindows, err = Maps(host, ".", indexMode, "push", true)
				if errors.Is(err, errCancelled) {
					return nil
				}
				if err != nil {
					return friendlyDiscoveryError(host, err)
				}
			}
			if !remoteIsWindows {
				remoteIsWindows = detectWindowsTarget(host, remoteDir)
			}
			profile := profileForHost(host)
			stabilityProfile := profile.RsyncStability
			if remoteIsWindows {
				remoteDir = ensureWindowsRemotePath(host, remoteDir)
			}

			source := normalizeLocalPathForRsync(localPath)
			targetDir := normalizeRemotePathForRsync(remoteDir)
			target := formatRemoteEndpoint(host, strings.TrimRight(targetDir, "/")+"/", remoteIsWindows)
			if err := runRsync(source, target, rsyncOptions{
				forceRemoteRsyncPath: remoteIsWindows || stabilityProfile,
				stabilityProfile:     stabilityProfile,
			}); err != nil {
				return err
			}
			return nil
		},
	}
}

func (a *app) resolveHostForTransfer(arg string) (string, error) {
	host := strings.TrimSpace(arg)
	var err error
	if host == "" {
		host, err = a.chooseHostAllowNew()
		if err != nil {
			return "", err
		}
	}

	if err := validateUserHost(host); err != nil {
		return "", fmt.Errorf("invalid host %q: %w", host, err)
	}
	if _, err := a.appendHostIfNew(host); err != nil {
		return "", fmt.Errorf("failed to save host history: %w", err)
	}
	return host, nil
}

func chooseManualValue(prompt, preset string) (string, error) {
	preset = strings.TrimSpace(preset)
	if preset != "" {
		return preset, nil
	}

	query, selected, err := selectOrQueryFZF(prompt, nil)
	if err != nil {
		return "", err
	}

	value := strings.TrimSpace(selected)
	if value == "" {
		value = strings.TrimSpace(query)
	}
	if value == "" {
		return "", errCancelled
	}
	return value, nil
}

func runLocalFindForPicker(base string, maxDepth int, dirsOnly bool) ([]string, error) {
	if _, err := exec.LookPath("find"); err != nil {
		return nil, fmt.Errorf("find not found in PATH: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve home directory: %w", err)
	}

	scanBase := filepath.Join(home, base)
	if filepath.IsAbs(base) {
		scanBase = base
	}
	scanBase = filepath.Clean(scanBase)
	ignoreRegex, _ := localIgnoreRegex(scanBase)
	quotedRegex := shellQuote(ignoreRegex)
	depthArg := ""
	if maxDepth > 0 {
		depthArg = fmt.Sprintf(" -maxdepth %d", maxDepth)
	}
	findCmd := fmt.Sprintf("find .%s -type d -print 2>/dev/null | grep -vE %s | sort", depthArg, quotedRegex)
	if !dirsOnly {
		findCmd = fmt.Sprintf("%s; find .%s -type f -print 2>/dev/null | grep -vE %s | sort", findCmd, depthArg, quotedRegex)
	}
	cmd := exec.Command("sh", "-c", fmt.Sprintf("cd %s && { %s; } || true", shellQuote(scanBase), findCmd))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(strings.ToLower(msg), "permission denied") {
			// Keep partial results so restricted folders do not break picker navigation.
		} else if msg == "" {
			msg = err.Error()
			return nil, fmt.Errorf("local path scan failed: %s", msg)
		} else {
			return nil, fmt.Errorf("local path scan failed: %s", msg)
		}
	}

	lines := strings.Split(strings.ReplaceAll(stdout.String(), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "." {
			continue
		}
		line = strings.TrimPrefix(filepath.ToSlash(line), "./")
		out = append(out, filepath.ToSlash(filepath.Join(scanBase, line)))
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errors.New("no local paths found")
	}
	return out, nil
}

func runRemoteFindForPicker(host, base string, maxDepth int, dirsOnly bool) ([]string, error) {
	paths, _, err := runRemoteFindForPickerDetailed(host, base, maxDepth, dirsOnly, false)
	return paths, err
}

func runRemoteFindForPickerDetailed(host, base string, maxDepth int, dirsOnly bool, fullIndex bool) ([]string, bool, error) {
	_ = maxDepth

	user, cleanHost := splitUserHost(host)
	action := "push"
	if !dirsOnly {
		action = "pull"
	}
	return getRemotePathsInternal(user, cleanHost, base, fullIndex, action)
}

func GetRemotePaths(user, host, remotePath string) ([]string, error) {
	paths, _, err := getRemotePathsInternal(user, host, remotePath, false, "pull")
	return paths, err
}

func GetRemoteLayer(user, host, remotePath string, fullIndex bool, action string) ([]string, bool, error) {
	return getRemotePathsInternal(user, host, remotePath, fullIndex, action)
}

func getRemotePathsInternal(user, host, remotePath string, fullIndex bool, action string) ([]string, bool, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, false, fmt.Errorf("ssh not found in PATH: %w", err)
	}

	target := strings.TrimSpace(host)
	if target == "" {
		return nil, false, errors.New("host is required")
	}
	user = strings.TrimSpace(user)
	if user != "" {
		target = user + "@" + target
	}

	base := strings.TrimSpace(remotePath)
	if base == "" {
		base = "."
	}
	ignoreRegex, customCount := remoteIgnoreRegex(target, base)
	logVerbose("Omni-Ignore Active: Filtering C/C++, Rust, Java, Python, and JS artifacts.")
	logVerbose("Specific Exclusions: first, compression, bot.js, fireworks.")
	logVerbose("Custom .gitignore: %d patterns merged", customCount)

	profile := profileForHost(target)
	autoWindows := false
	if !profile.UseUnixDiscovery {
		autoWindows = detectWindowsTarget(target, base)
	}
	remoteCmd := buildRemoteDiscoveryCommand(base, fullIndex, action, ignoreRegex, profile.UseUnixDiscovery, autoWindows)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := buildSSHCommand(ctx, target, false, remoteCmd)
	logVerbose("remote discovery command for %s: %s", target, remoteCmd)
	logVerbose("ssh invocation: %s", formatCommand(cmd.Path, cmd.Args[1:]))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, fmt.Errorf("failed to attach stdout pipe for %s: %w", target, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, false, fmt.Errorf("failed to attach stderr pipe for %s: %w", target, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("failed to start remote path scan on %s: %w", target, err)
	}

	detectedWindows := false
	var detectedMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scannerErr := bufio.NewScanner(stderr)
		scannerErr.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scannerErr.Scan() {
			if looksLikeWindowsSignal(scannerErr.Text()) {
				detectedMu.Lock()
				detectedWindows = true
				detectedMu.Unlock()
			}
			_, skip := cleanRemoteDiscoveryLine(scannerErr.Text())
			if skip {
				continue
			}
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := make([]string, 0, 128)
	for scanner.Scan() {
		raw := scanner.Text()
		if looksLikeWindowsSignal(raw) {
			detectedMu.Lock()
			detectedWindows = true
			detectedMu.Unlock()
		}
		clean, skip := cleanRemoteDiscoveryLine(raw)
		if looksLikeWindowsPath(clean) {
			detectedMu.Lock()
			detectedWindows = true
			detectedMu.Unlock()
		}
		if skip {
			continue
		}
		normalized, ok := normalizeDiscoveryEntry(clean, base)
		if !ok {
			continue
		}
		out = append(out, normalized)
	}
	if err := scanner.Err(); err != nil {
		wg.Wait()
		_ = cmd.Wait()
		return nil, false, fmt.Errorf("stdout scanner failed on %s: %w", target, err)
	}

	wg.Wait()
	if err := cmd.Wait(); err != nil {
		detectedMu.Lock()
		windowsHost := detectedWindows
		detectedMu.Unlock()
		if isExitStatus255(err) && len(out) > 0 {
			return dedupeKeepOrder(out), windowsHost, nil
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, false, fmt.Errorf("remote path scan timed out after 10s on %s", target)
		}
		return nil, false, fmt.Errorf("remote path scan failed on %s: %w", target, err)
	}

	detectedMu.Lock()
	windowsHost := detectedWindows
	detectedMu.Unlock()
	return dedupeKeepOrder(out), windowsHost, nil
}

func buildRemoteDiscoveryCommand(remotePath string, fullIndex bool, action string, ignoreRegex string, forceUnix bool, autoWindows bool) string {
	if forceUnix || !autoWindows {
		return buildUnixDiscoveryCommand(remotePath, fullIndex, action, ignoreRegex)
	}
	return buildWindowsDiscoveryCommand(remotePath, action)
}

func buildUnixDiscoveryCommand(remotePath string, fullIndex bool, action string, ignoreRegex string) string {
	base := strings.TrimSpace(remotePath)
	if base == "" {
		base = "."
	}
	if strings.TrimSpace(ignoreRegex) == "" {
		ignoreRegex = getGlobalIgnoreRegex()
	}
	quotedRegex := shellQuote(ignoreRegex)

	dirDepth := " -maxdepth 1"
	fileDepth := " -maxdepth 1"
	if fullIndex {
		dirDepth = fmt.Sprintf(" -maxdepth %d", fullIndexDepth)
		fileDepth = fmt.Sprintf(" -maxdepth %d", fullIndexDepth)
		logVerbose("remote full index mode: dir depth=%d, file depth=%d, excludes=%s", fullIndexDepth, fullIndexDepth, ignoreRegex)
	} else {
		logVerbose("remote lazy index mode: dir depth=1, file depth=1, excludes=%s", ignoreRegex)
	}

	findDirs := fmt.Sprintf("find .%s -type d -not -path '.' -print 2>/dev/null | grep -vE %s | sort | sed 's#^\\./##; s#$#/#'", dirDepth, quotedRegex)
	if strings.EqualFold(strings.TrimSpace(action), "pull") {
		findFiles := fmt.Sprintf("find .%s -type f -print 2>/dev/null | grep -vE %s | sort | sed 's#^\\./##'", fileDepth, quotedRegex)
		return fmt.Sprintf("cd %s && { (%s; %s) || true; } | head -n 500 || true", shellQuote(base), findDirs, findFiles)
	}
	return fmt.Sprintf("cd %s && { (%s) || true; } | head -n 500 || true", shellQuote(base), findDirs)
}

func buildWindowsDiscoveryCommand(remotePath, action string) string {
	base := strings.TrimSpace(remotePath)
	if base == "" || base == "." {
		if strings.EqualFold(strings.TrimSpace(action), "pull") {
			return "dir /b"
		}
		return "dir /ad /b"
	}
	if strings.EqualFold(strings.TrimSpace(action), "pull") {
		return fmt.Sprintf("cd %s 2>nul && dir /b || dir /b", shellQuote(base))
	}
	return fmt.Sprintf("cd %s 2>nul && dir /ad /b || dir /ad /b", shellQuote(base))
}

func cleanRemoteDiscoveryLine(raw string) (string, bool) {
	clean := strings.TrimSpace(ansiCSI.ReplaceAllString(raw, ""))
	clean = strings.TrimSpace(strings.TrimRight(clean, `\`))
	if clean == "" {
		return "", true
	}

	lower := strings.ToLower(clean)
	if strings.Contains(lower, "shared connection to ") && strings.Contains(lower, " closed") {
		return clean, true
	}
	if strings.Contains(lower, "volume in drive") {
		return clean, true
	}
	if strings.Contains(lower, "directory of") {
		return clean, true
	}
	return clean, false
}

func normalizeDiscoveryEntry(entry, base string) (string, bool) {
	entry = strings.TrimSpace(strings.ReplaceAll(entry, `\`, `/`))
	if entry == "" {
		return "", false
	}

	isDir := strings.HasSuffix(entry, "/")
	entry = strings.TrimSuffix(entry, "/")
	base = strings.TrimSpace(strings.ReplaceAll(base, `\`, `/`))
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		base = "."
	}

	if entry == "." || entry == base {
		return "", false
	}
	if strings.HasPrefix(entry, base+"/") {
		entry = strings.TrimPrefix(entry, base+"/")
	}
	if strings.HasPrefix(entry, "./") {
		entry = strings.TrimPrefix(entry, "./")
	}
	if entry == "" || entry == "." {
		return "", false
	}
	if isDir {
		entry += "/"
	}
	return entry, true
}

func looksLikeWindowsSignal(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "conhost.exe")
}

func isExitStatus255(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == 255
}

func runRemoteFindForPickerWithFallback(host string, maxDepth int, dirsOnly bool) ([]string, error) {
	candidates := []string{
		defaultRemoteWindowsBase(host),
		"/C/Users",
		".",
		"/",
	}

	seen := make(map[string]struct{}, len(candidates))
	var lastErr error
	for _, base := range candidates {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		if _, ok := seen[base]; ok {
			continue
		}
		seen[base] = struct{}{}

		paths, err := runRemoteFindForPicker(host, base, maxDepth, dirsOnly)
		if err == nil && len(paths) > 0 {
			return paths, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no remote paths found on %s", host)
	}
	return nil, lastErr
}

func runRemoteFindForPickerWithFallbackDetailed(host string, maxDepth int, dirsOnly bool, fullIndex bool) ([]string, bool, error) {
	candidates := []string{
		defaultRemoteWindowsBase(host),
		"/C/Users",
		".",
		"/",
	}

	seen := make(map[string]struct{}, len(candidates))
	var lastErr error
	detectedWindows := false
	for _, base := range candidates {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		if _, ok := seen[base]; ok {
			continue
		}
		seen[base] = struct{}{}

		paths, windowsHost, err := runRemoteFindForPickerDetailed(host, base, maxDepth, dirsOnly, fullIndex)
		detectedWindows = detectedWindows || windowsHost
		if err == nil && len(paths) > 0 {
			return paths, detectedWindows, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no remote paths found on %s", host)
	}
	return nil, detectedWindows, lastErr
}

func defaultRemoteWindowsBase(host string) string {
	user, _ := splitUserHost(host)
	if user != "" {
		return "/C/Users/" + strings.TrimSpace(user)
	}
	return "/C/Users"
}

func splitUserHost(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, "@", 2)
	if len(parts) != 2 {
		return "", raw
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func Maps(host, startPath, indexMode, action string, isRemote bool) (string, bool, error) {
	indexMode = normalizeRemoteIndexMode(indexMode)
	fullIndex := indexMode == "full"
	currentPath := strings.TrimSpace(startPath)
	if isRemote {
		if currentPath == "" {
			currentPath = "."
		}
	} else {
		resolvedLocal, err := resolveLocalNavigatorPath(currentPath)
		if err != nil {
			return "", false, err
		}
		currentPath = resolvedLocal
	}

	user, cleanHost := splitUserHost(host)
	detectedWindows := false
	sourceSelection := isSourceSelection(isRemote, action)
	destinationSelection := isDestinationSelection(isRemote, action)

	for {
		var (
			items       []string
			windowsHost bool
			err         error
		)
		if isRemote {
			items, windowsHost, err = GetRemoteLayer(user, cleanHost, currentPath, fullIndex, action)
			if err != nil {
				return "", detectedWindows, err
			}
			detectedWindows = detectedWindows || windowsHost
		} else {
			includeFiles := !strings.EqualFold(strings.TrimSpace(action), "pull")
			items, err = getLocalLayer(currentPath, includeFiles, fullIndex)
			if err != nil {
				return "", false, err
			}
		}

		choices := make([]string, 0, len(items)+3)
		contentsToken := ""
		entireToken := ""
		confirm := "[SYNC TO THIS FOLDER]"
		if sourceSelection {
			contentsToken, entireToken = syncSelectionTokens(currentPath, isRemote, destinationSelection)
			choices = append(choices, contentsToken)
			if entireToken != "" {
				choices = append(choices, entireToken)
			}
		} else {
			choices = append(choices, confirm)
		}
		choices = append(choices, "..")
		if len(items) == 0 {
			if isRemote {
				choices = append(choices, "(No subdirectories found)")
			} else {
				choices = append(choices, "(No entries found)")
			}
		} else {
			choices = append(choices, items...)
		}

		prompt := localNavigatorPrompt(currentPath)
		if isRemote {
			prompt = navigatorPrompt(host, currentPath)
		}
		selected, err := selectFromFZF(prompt, choices)
		if err != nil {
			if errors.Is(err, errCancelled) {
				return "", detectedWindows, errCancelled
			}
			return "", detectedWindows, err
		}

		switch strings.TrimSpace(selected) {
		case confirm:
			if isRemote {
				return ensureTrailingSlashForMode(normalizeRemotePathForRsync(currentPath), true), detectedWindows, nil
			}
			return ensureTrailingSlashForMode(currentPath, false), false, nil
		case contentsToken:
			if isRemote {
				return ensureTrailingSlashForMode(normalizeRemotePathForRsync(currentPath), true), detectedWindows, nil
			}
			return ensureTrailingSlashForMode(currentPath, false), false, nil
		case entireToken:
			if isRemote {
				return strings.TrimSuffix(normalizeRemotePathForRsync(currentPath), "/"), detectedWindows, nil
			}
			clean := filepath.Clean(currentPath)
			if clean == string(filepath.Separator) {
				return clean, false, nil
			}
			return strings.TrimSuffix(clean, string(filepath.Separator)), false, nil
		case "..":
			if isRemote {
				currentPath = remoteParentPath(currentPath)
			} else {
				currentPath = localParentPath(currentPath)
			}
		case "(No subdirectories found)":
			continue
		case "(No entries found)":
			continue
		default:
			isDir := strings.HasSuffix(selected, "/")
			name := strings.TrimSuffix(selected, "/")
			var nextPath string
			if isRemote {
				nextPath = remoteJoinPath(currentPath, name)
			} else {
				nextPath = filepath.Join(currentPath, name)
			}
			if !isDir {
				if sourceSelection {
					if isRemote {
						return normalizeRemotePathForRsync(nextPath), detectedWindows, nil
					}
					return nextPath, false, nil
				}
				continue
			}
			currentPath = nextPath
		}
	}
}

func isSourceSelection(isRemote bool, action string) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	return (!isRemote && action == "push") || (isRemote && action == "pull")
}

func isDestinationSelection(isRemote bool, action string) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	return (!isRemote && action == "pull") || (isRemote && action == "push")
}

func ensureTrailingSlashForMode(raw string, isRemote bool) string {
	if isRemote {
		raw = normalizeRemotePathForRsync(raw)
		if raw == "" {
			return "./"
		}
		if !strings.HasSuffix(raw, "/") {
			raw += "/"
		}
		return raw
	}
	clean := filepath.Clean(raw)
	if clean == string(filepath.Separator) {
		return clean
	}
	if !strings.HasSuffix(clean, string(filepath.Separator)) {
		clean += string(filepath.Separator)
	}
	return clean
}

func syncSelectionTokens(currentPath string, isRemote bool, isDestination bool) (string, string) {
	if isRemote {
		base := normalizeRemotePathForRsync(currentPath)
		base = strings.TrimSuffix(base, "/")
		if base == "" {
			base = "."
		}
		if isDestination {
			return "[SYNC CONTENTS (Files Only)]", ""
		}
		return "[SYNC CONTENTS (Files Only)]", "[SYNC ENTIRE DIR (Recursive)]"
	}
	if isDestination {
		return "[SYNC CONTENTS (Files Only)]", ""
	}
	return "[SYNC CONTENTS (Files Only)]", "[SYNC ENTIRE DIR (Recursive)]"
}

func normalizeRemoteIndexMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "full" {
		return mode
	}
	return "lazy"
}

func getGlobalIgnoreRegex() string {
	return `(\\.o$|\\.obj$|\\.a$|\\.lib$|\\.so$|\\.dll$|\\.dylib$|\\.out$|\\.exe$|target(/|$)|build(/|$)|CMakeFiles(/|$)|CMakeCache\\.txt$|ipch(/|$)|\\.vs(/|$)|\\.pdb$|\\.d$|first$|compression$|.*\\.bin$|.*\\.acomp$|\\.class$|\\.jar$|\\.war$|\\.ear$|\\.metadata(/|$)|\\.recommenders(/|$)|\\.gradle(/|$)|bin(/|$)|obj(/|$)|lib(/|$)|include(/|$)|share(/|$)|node_modules(/|$)|\\.venv(/|$)|env(/|$)|venv(/|$)|ENV(/|$)|__pycache__(/|$)|\\.pyc$|\\.parcel-cache(/|$)|dist(/|$)|\\.yarn(/|$)|package-lock\\.json$|\\.git(/|$)|\\.vscode(/|$)|\\.idea(/|$)|\\.DS_Store$|Thumbs\\.db$|bot\\.js$|fireworks(/|$))`
}

func parseGitignorePatterns(content string) []string {
	lines := strings.Split(content, "\n")
	parts := make([]string, 0, len(lines))
	seen := map[string]struct{}{}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		line = strings.TrimPrefix(line, "./")
		line = strings.TrimPrefix(line, "/")
		line = strings.TrimSuffix(line, "/")
		line = strings.TrimSuffix(line, "/*")
		if line == "" || strings.ContainsAny(line, "*?[]{}") {
			continue
		}

		token := regexp.QuoteMeta(line)
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		parts = append(parts, token)
	}

	sort.Strings(parts)
	return parts
}

func buildRegexFromPatterns(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	return "(" + strings.Join(patterns, "|") + ")"
}

func mergeIgnorePatterns(globalRegex string, custom []string) string {
	customRegex := buildRegexFromPatterns(custom)
	customRegex = strings.TrimSpace(customRegex)
	if customRegex == "" {
		return globalRegex
	}
	return globalRegex[:len(globalRegex)-1] + "|" + strings.Trim(customRegex, "()") + ")"
}

func localIgnoreRegex(basePath string) (string, int) {
	global := getGlobalIgnoreRegex()
	raw, err := os.ReadFile(filepath.Join(basePath, ".gitignore"))
	if err != nil {
		return global, 0
	}
	custom := parseGitignorePatterns(string(raw))
	return mergeIgnorePatterns(global, custom), len(custom)
}

func remoteIgnoreRegex(target, basePath string) (string, int) {
	global := getGlobalIgnoreRegex()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := buildSSHCommand(ctx, target, false, fmt.Sprintf("cd %s && [ -f .gitignore ] && cat .gitignore || true", shellQuote(basePath)))
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return global, 0
	}
	custom := parseGitignorePatterns(out.String())
	return mergeIgnorePatterns(global, custom), len(custom)
}

func getLocalLayer(current string, includeFiles bool, fullIndex bool) ([]string, error) {
	resolvedCurrent, err := resolveLocalNavigatorPath(current)
	if err != nil {
		return nil, err
	}
	ignoreRegex, customCount := localIgnoreRegex(resolvedCurrent)
	logVerbose("Omni-Ignore Active: Filtering C/C++, Rust, Java, Python, and JS artifacts.")
	logVerbose("Specific Exclusions: first, compression, bot.js, fireworks.")
	logVerbose("Custom .gitignore: %d patterns merged", customCount)
	if fullIndex {
		logVerbose("local index mode: depth=%d, excludes=%s", fullIndexDepth, ignoreRegex)
	} else {
		logVerbose("local index mode: dir depth=1, file depth=1, excludes=%s", ignoreRegex)
	}
	filterRE, err := regexp.Compile(ignoreRegex)
	if err != nil {
		filterRE = regexp.MustCompile(getGlobalIgnoreRegex())
	}
	globalRE := regexp.MustCompile(getGlobalIgnoreRegex())
	shouldIgnore := func(name, rel string) bool {
		return filterRE.MatchString(name) || filterRE.MatchString(rel)
	}

	if fullIndex {
		out := make([]string, 0, 512)
		walkErr := filepath.WalkDir(resolvedCurrent, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if errors.Is(walkErr, os.ErrPermission) {
					if d != nil && d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				return walkErr
			}
			if p == resolvedCurrent {
				return nil
			}
			rel, err := filepath.Rel(resolvedCurrent, p)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			depth := localRelativeDepth(rel)
			if depth > fullIndexDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if d.IsDir() && (globalRE.MatchString(name) || globalRE.MatchString(rel)) {
				return filepath.SkipDir
			}
			if shouldIgnore(name, rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				out = append(out, rel+"/")
				return nil
			}
			if includeFiles {
				out = append(out, rel)
			}
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("failed to read local directory %s: %w", resolvedCurrent, walkErr)
		}
		sort.Strings(out)
		return out, nil
	}

	entries, err := os.ReadDir(resolvedCurrent)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read local directory %s: %w", resolvedCurrent, err)
	}

	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		if shouldIgnore(name, name) {
			continue
		}
		if entry.IsDir() {
			out = append(out, name+"/")
			continue
		}
		if includeFiles {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func localRelativeDepth(rel string) int {
	rel = strings.Trim(strings.TrimSpace(rel), "/")
	if rel == "" || rel == "." {
		return 0
	}
	return strings.Count(rel, "/") + 1
}

func localParentPath(current string) string {
	current = filepath.Clean(current)
	parent := filepath.Dir(current)
	if parent == "" {
		return string(filepath.Separator)
	}
	return parent
}

func localNavigatorPrompt(current string) string {
	resolved, err := resolveLocalNavigatorPath(current)
	if err != nil {
		return "[Local] Path: ~/ "
	}
	return fmt.Sprintf("[Local] Path: %s ", formatLocalBreadcrumb(resolved))
}

func resolveLocalNavigatorPath(current string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}

	current = strings.TrimSpace(current)
	if current == "" || current == "." {
		return filepath.Clean(home), nil
	}
	current, err = expandUserPath(current)
	if err != nil {
		return "", fmt.Errorf("failed to resolve local path %s: %w", current, err)
	}
	if !filepath.IsAbs(current) {
		current = filepath.Join(home, current)
	}
	return filepath.Clean(current), nil
}

func formatLocalBreadcrumb(absPath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.ToSlash(absPath)
	}

	absPath = filepath.Clean(absPath)
	home = filepath.Clean(home)
	rel, relErr := filepath.Rel(home, absPath)
	if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if rel == "." {
			return "~/"
		}
		return "~/" + filepath.ToSlash(rel)
	}
	return filepath.ToSlash(absPath)
}

func remoteParentPath(current string) string {
	current = normalizeRemotePathForRsync(current)
	if current == "" || current == "." || current == "/" {
		return "."
	}
	parent := path.Dir(current)
	if parent == "." || parent == "/" {
		return "."
	}
	return parent
}

func remoteJoinPath(current, child string) string {
	current = normalizeRemotePathForRsync(current)
	child = normalizeRemotePathForRsync(child)

	if child == "" || child == "." {
		return current
	}
	if strings.HasPrefix(child, "/") || looksLikeWindowsPath(child) {
		return child
	}
	if current == "" || current == "." || current == "/" {
		return strings.TrimPrefix(child, "./")
	}
	return path.Clean(path.Join(current, child))
}

func looksLikeUserHost(raw string) bool {
	if strings.Count(raw, "@") != 1 {
		return false
	}
	parts := strings.SplitN(raw, "@", 2)
	user := strings.TrimSpace(parts[0])
	host := strings.TrimSpace(parts[1])
	if user == "" || host == "" {
		return false
	}
	if !userPattern.MatchString(user) {
		return false
	}

	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return net.ParseIP(host) != nil || hostPattern.MatchString(host)
}

func (a *app) newHostCmd() *cobra.Command {
	hostCmd := &cobra.Command{
		Use:   "host",
		Short: "Manage host history",
	}

	hostCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List known hosts",
		RunE: func(cmd *cobra.Command, args []string) error {
			hosts, err := a.readHosts()
			if err != nil {
				return err
			}
			for _, h := range hosts {
				fmt.Println(h)
			}
			return nil
		},
	})

	hostCmd.AddCommand(&cobra.Command{
		Use:   "add [user@ip]",
		Short: "Add host to history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := strings.TrimSpace(args[0])
			if err := validateUserHost(host); err != nil {
				return fmt.Errorf("invalid host format: %w", err)
			}

			added, err := a.appendHostIfNew(host)
			if err != nil {
				return err
			}
			if !added {
				fmt.Println("host already exists")
				return nil
			}
			fmt.Println("host added")
			return nil
		},
	})

	hostCmd.AddCommand(&cobra.Command{
		Use:     "remove [user@ip]",
		Aliases: []string{"rm"},
		Short:   "Remove host from history",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.TrimSpace(args[0])
			hosts, err := a.readHosts()
			if err != nil {
				return err
			}

			updated := make([]string, 0, len(hosts))
			removed := false
			for _, h := range hosts {
				if h == target {
					removed = true
					continue
				}
				updated = append(updated, h)
			}
			if !removed {
				fmt.Println("host not found")
				return nil
			}
			if err := a.writeHosts(updated); err != nil {
				return err
			}
			fmt.Println("host removed")
			return nil
		},
	})

	return hostCmd
}

func (a *app) chooseHostAllowNew() (string, error) {
	hosts, err := a.readHosts()
	if err != nil {
		return "", fmt.Errorf("failed to read host history: %w", err)
	}

	query, selected, err := selectOrQueryFZF("ssh host> ", hosts)
	if err != nil {
		return "", err
	}

	// Prefer explicit selection from fzf; fall back to raw query for new entries.
	candidate := strings.TrimSpace(selected)
	if candidate == "" {
		candidate = strings.TrimSpace(query)
	}
	candidate = strings.TrimSpace(strings.ReplaceAll(candidate, "\r", ""))
	if candidate == "" {
		return "", errCancelled
	}
	return candidate, nil
}

func (a *app) chooseKnownHost(prompt string) (string, error) {
	hosts, err := a.readHosts()
	if err != nil {
		return "", err
	}
	if len(hosts) == 0 {
		return "", errors.New("no hosts in history; add one with `nexus host add user@ip` or use `nexus ssh`")
	}

	return selectFromFZF(prompt, hosts)
}

func selectFromFZF(prompt string, options []string) (string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", errors.New("fzf not found in PATH")
	}

	if len(options) == 0 {
		return "", errCancelled
	}

	args := []string{
		"--height", "40%",
		"--layout", "reverse",
		"--border",
		"--prompt", prompt,
	}

	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(strings.Join(options, "\n"))
	cmd.Stderr = os.Stderr

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		if isFZFCancel(err) {
			return "", errCancelled
		}
		return "", fmt.Errorf("fzf failed: %w", err)
	}

	selected := strings.TrimSpace(out.String())
	if selected == "" {
		return "", errCancelled
	}
	return selected, nil
}

func selectOrQueryFZF(prompt string, options []string) (string, string, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return "", "", errors.New("fzf not found in PATH")
	}

	args := []string{
		"--height", "40%",
		"--layout", "reverse",
		"--border",
		"--prompt", prompt,
		"--print-query",
		"--bind", "enter:accept",
	}

	joined := strings.Join(options, "\n")
	if joined == "" {
		joined = "\n"
	}

	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(joined)
	cmd.Stderr = os.Stderr

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		if isFZFCancel(err) {
			return "", "", errCancelled
		}
		return "", "", fmt.Errorf("fzf failed: %w", err)
	}

	output := strings.ReplaceAll(out.String(), "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")
	if output == "" {
		return "", "", errCancelled
	}

	lines := strings.Split(output, "\n")
	if len(lines) == 0 {
		return "", "", errCancelled
	}

	query := strings.TrimSpace(lines[0])
	selected := ""
	if len(lines) > 1 {
		selected = strings.TrimSpace(lines[1])
	}

	if query == "" && selected == "" {
		return "", "", errCancelled
	}
	return query, selected, nil
}

func isFZFCancel(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return code == 1 || code == 130
	}
	return false
}

func buildSSHCommand(ctx context.Context, host string, interactive bool, remoteCmd string) *exec.Cmd {
	args := []string{
		"-o", "ConnectTimeout=" + connectTimeoutSeconds,
		"-o", "LogLevel=ERROR",
		"-o", "VisualHostKey=no",
		"-q",
	}
	if interactive {
		args = append([]string{"-t", "-t"}, args...)
	} else {
		args = append(args, "-o", "StrictHostKeyChecking=accept-new", "-T")
	}
	args = append(args, host)
	if strings.TrimSpace(remoteCmd) != "" {
		args = append(args, remoteCmd)
	}
	if ctx == nil {
		return exec.Command("ssh", args...)
	}
	return exec.CommandContext(ctx, "ssh", args...)
}

func runInteractiveSSH(host string) error {
	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("ssh not found in PATH: %w", err)
	}

	cmd := buildSSHCommand(nil, host, true, "")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func listRemotePaths(host string) ([]string, error) {
	remoteCmd := remoteBasePathScript() + `; find "$base" -maxdepth 3 \( -path "*/.git" -o -path "*/.git/*" \) -prune -o -mindepth 1 -print 2>/dev/null`
	return runRemoteFind(host, remoteCmd)
}

func listRemoteDirs(host string) ([]string, error) {
	remoteCmd := remoteBasePathScript() + `; find "$base" \( -path "*/.git" -o -path "*/.git/*" \) -prune -o -type d -print 2>/dev/null`
	return runRemoteFind(host, remoteCmd)
}

func remoteBasePathScript() string {
	return `base="${HOME:-.}"; if command -v cygpath >/dev/null 2>&1 && [ -n "${USERPROFILE:-}" ]; then up=$(cygpath -u "$USERPROFILE" 2>/dev/null); if [ -n "$up" ]; then base="$up"; fi; fi`
}

func runRemoteFind(host, remoteCmd string) ([]string, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, fmt.Errorf("ssh not found in PATH: %w", err)
	}

	cmd := buildSSHCommand(nil, host, false, remoteCmd)
	logVerbose("remote find command for %s: %s", host, remoteCmd)
	logVerbose("ssh invocation: %s", formatCommand(cmd.Path, cmd.Args[1:]))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		errText := strings.ToLower(stderrText)
		if errText == "" {
			errText = strings.ToLower(err.Error())
		}
		if isNetworkUnreachableError(errText) {
			return nil, errHostUnreachable
		}
		if stderrText == "" {
			stderrText = err.Error()
		}
		return nil, fmt.Errorf("host lookup failed for %s: %s", host, stderrText)
	}

	lines := strings.Split(strings.ReplaceAll(stdout.String(), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func detectWindowsTarget(host, candidatePath string) bool {
	if looksLikeWindowsPath(candidatePath) {
		return true
	}
	isWindows, err := probeRemoteWindowsHost(host)
	return err == nil && isWindows
}

func probeRemoteWindowsHost(host string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Probe both Unix-like and Windows-native signals. If MSYS/CYGWIN/MINGW is present,
	// prefer Unix discovery because find/sort pipelines work there.
	cmd := buildSSHCommand(ctx, host, false, "(uname -s 2>/dev/null || true); (command -v find >/dev/null 2>&1 && command -v sort >/dev/null 2>&1 && echo UNIX_TOOLS) || true; (cmd /c ver >NUL 2>&1 && echo WINDOWS_NATIVE) || (ver >NUL 2>&1 && echo WINDOWS_NATIVE) || true")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return false, fmt.Errorf("remote probe failed: %s", msg)
	}
	probeOut := strings.ToUpper(stdout.String())
	if strings.Contains(probeOut, "MSYS") || strings.Contains(probeOut, "MINGW") || strings.Contains(probeOut, "CYGWIN") {
		return false, nil
	}
	if strings.Contains(probeOut, "UNIX_TOOLS") {
		return false, nil
	}
	return strings.Contains(probeOut, "WINDOWS_NATIVE"), nil
}

func navigatorPrompt(host, currentPath string) string {
	current := normalizeRemotePathForRsync(currentPath)
	if current == "" || current == "." {
		current = "."
	}
	if !strings.HasSuffix(current, "/") {
		current += "/"
	}
	return fmt.Sprintf("Navigating: %s:%s ", host, current)
}

func isNetworkUnreachableError(msg string) bool {
	needle := []string{
		"timed out",
		"no route to host",
		"connection refused",
		"could not resolve hostname",
		"name or service not known",
		"network is unreachable",
		"host is down",
	}
	for _, n := range needle {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

type rsyncOptions struct {
	forceRemoteRsyncPath bool
	stabilityProfile     bool
}

func runRsync(source, destination string, opts rsyncOptions) error {
	rsyncBin, err := resolveRsyncBinary()
	if err != nil {
		return err
	}

	source = normalizeRemoteEndpointPath(source)
	destination = normalizeRemoteEndpointPath(destination)

	args := buildRsyncArgs(source, destination, opts)

	if err := runRsyncCommand(rsyncBin, args); err != nil {
		// Mixed Windows/MSYS/OpenSSH stacks can intermittently fail with code 12.
		// Retry once with stability profile enabled.
		if isExitCode(err, 12) && !opts.stabilityProfile {
			retryOpts := opts
			retryOpts.stabilityProfile = true
			retryOpts.forceRemoteRsyncPath = true
			retryArgs := buildRsyncArgs(source, destination, retryOpts)
			logVerbose("rsync code 12 detected; retrying with stability profile: %s", formatCommand(rsyncBin, retryArgs))
			if retryErr := runRsyncCommand(rsyncBin, retryArgs); retryErr == nil {
				return nil
			}
		}
		return fmt.Errorf("rsync failed (%s -> %s): %w", source, destination, err)
	}
	return nil
}

func buildRsyncArgs(source, destination string, opts rsyncOptions) []string {
	args := []string{
		"-av",
		// If discovery + transfer are separate SSH sessions, password auth can prompt twice.
		// Consider ControlMaster/ControlPersist in ~/.ssh/config to reuse one connection.
		"-e", "ssh -o VisualHostKey=no",
		"--blocking-io",
	}
	if opts.forceRemoteRsyncPath {
		args = append(args, "--rsync-path=rsync")
	}
	if opts.stabilityProfile {
		args = append(args, "--bwlimit=2048", "--protocol=30")
	}
	if opts.forceRemoteRsyncPath || opts.stabilityProfile {
		// Keep compression off on Windows stability profile targets.
	} else {
		args = append(args, "-z")
	}
	args = append(args,
		"--no-p",
		"--no-g",
		"--chmod=ugo=rwX",
	)
	args = append(args, source, destination)
	return args
}

func runRsyncCommand(rsyncBin string, args []string) error {
	cmd := exec.Command(rsyncBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == code
}

func resolveRsyncBinary() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("NEXUS_RSYNC_PATH")); custom != "" {
		path, err := exec.LookPath(custom)
		if err != nil {
			return "", fmt.Errorf("NEXUS_RSYNC_PATH is set but unusable (%s): %w", custom, err)
		}
		return path, nil
	}

	candidates := []string{
		"/opt/homebrew/bin/rsync",
		"/usr/local/bin/rsync",
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		return candidate, nil
	}

	path, err := exec.LookPath("rsync")
	if err != nil {
		return "", fmt.Errorf("rsync not found (checked NEXUS_RSYNC_PATH, Homebrew paths, and PATH): %w", err)
	}
	return path, nil
}

func normalizeRemoteEndpointPath(endpoint string) string {
	host, remotePath, ok := strings.Cut(endpoint, ":")
	if !ok || !strings.Contains(host, "@") {
		return endpoint
	}
	if strings.Contains(remotePath, "'") || strings.Contains(remotePath, "\"") {
		return endpoint
	}
	return host + ":" + normalizeRemotePathForRsync(remotePath)
}

func formatRemoteEndpoint(host, remotePath string, quoteForWindows bool) string {
	normalized := normalizeRemotePathForRsync(remotePath)
	if quoteForWindows && strings.ContainsAny(normalized, " \t") {
		normalized = shellQuote(normalized)
	}
	return fmt.Sprintf("%s:%s", host, normalized)
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
	if localName == "" || localName == "." || localName == "/" {
		return
	}

	openCmd := exec.Command("open", localName)
	openCmd.Stdin = os.Stdin
	openCmd.Stdout = os.Stdout
	openCmd.Stderr = os.Stderr
	_ = openCmd.Run()
}

func expandUserPath(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func normalizeLocalPathForRsync(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}

	path = strings.ReplaceAll(path, `\`, `/`)
	if len(path) >= 2 && path[1] == ':' {
		drive := strings.ToLower(string(path[0]))
		rest := path[2:]
		if !strings.HasPrefix(rest, "/") {
			rest = "/" + rest
		}
		return "/" + drive + rest
	}
	return path
}

func normalizeRemotePathForRsync(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, `\`, `/`))
	if len(path) >= 2 && path[1] == ':' {
		drive := strings.ToUpper(string(path[0]))
		rest := strings.TrimPrefix(path[2:], "/")
		if rest == "" {
			return "/" + drive
		}
		return "/" + drive + "/" + rest
	}
	return path
}

func looksLikeWindowsPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	if strings.Contains(lower, "conhost.exe") {
		return true
	}
	if strings.Contains(lower, `\users\`) {
		return true
	}
	if len(path) >= 2 && path[1] == ':' {
		return true
	}
	if len(path) >= 3 && path[0] == '/' && ((path[1] >= 'A' && path[1] <= 'Z') || (path[1] >= 'a' && path[1] <= 'z')) && path[2] == '/' {
		return true
	}
	return strings.HasPrefix(lower, "/c/users/")
}

func ensureWindowsRemotePath(host, remotePath string) string {
	base := defaultRemoteWindowsBase(host)
	remotePath = normalizeRemotePathForRsync(remotePath)
	remotePath = strings.TrimSpace(remotePath)

	switch remotePath {
	case "", ".":
		return base
	}
	if strings.HasPrefix(remotePath, "/") || looksLikeWindowsPath(remotePath) {
		return remotePath
	}
	if strings.HasPrefix(remotePath, "~/") {
		return strings.TrimRight(base, "/") + "/" + strings.TrimPrefix(remotePath, "~/")
	}
	remotePath = strings.TrimPrefix(remotePath, "./")
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(remotePath, "/")
}

func friendlyDiscoveryError(host string, err error) error {
	msg := strings.ToLower(err.Error())
	if isNetworkUnreachableError(msg) || strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "timed out") {
		return fmt.Errorf("unable to connect to %s while indexing remote directories; check SSH connectivity and retry", host)
	}
	return fmt.Errorf("failed to index remote directories on %s: %w", host, err)
}

func profileForHost(host string) discoveryProfile {
	_, cleanHost := splitUserHost(host)
	if cleanHost == "" {
		cleanHost = host
	}
	cleanHost = strings.Trim(strings.ToLower(strings.TrimSpace(cleanHost)), "[]")
	if profile, ok := hostDiscoveryProfiles[cleanHost]; ok {
		return profile
	}
	return discoveryProfile{}
}

func logVerbose(format string, args ...any) {
	if !verboseLogging {
		return
	}
	fmt.Fprintf(os.Stderr, "[nexus][verbose] "+format+"\n", args...)
}

func formatCommand(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(name))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`!&*()[]{}<>?|;") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func validateUserHost(raw string) error {
	if strings.Count(raw, "@") != 1 {
		return validationErr("expected exactly one '@' in user@ip format")
	}

	parts := strings.SplitN(raw, "@", 2)
	user := strings.TrimSpace(parts[0])
	host := strings.TrimSpace(parts[1])
	if user == "" || host == "" {
		return validationErr("user and host must both be non-empty")
	}
	if !userPattern.MatchString(user) {
		return validationErr("username contains unsupported characters")
	}

	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	if net.ParseIP(host) != nil {
		return nil
	}
	if hostPattern.MatchString(host) {
		return nil
	}
	return validationErr("host must be a valid IP address or hostname")
}

func validationErr(msg string) error {
	return errors.New(msg)
}

func dedupeKeepOrder(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
