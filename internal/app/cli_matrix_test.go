package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type cliMatrixFixture struct {
	configDir  string
	configFile string
	hostsFile  string
	stateFile  string
	localFile  string
	logFile    string
}

func TestCLICommandAndAliasInventory(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	root := fixture.app().newRootCmd()

	got := make([]string, 0)
	aliases := make(map[string][]string)
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, command := range parent.Commands() {
			if command.Name() == "help" {
				continue
			}
			path := strings.TrimPrefix(command.CommandPath(), "nexus ")
			got = append(got, path)
			if len(command.Aliases) > 0 {
				aliases[path] = append([]string(nil), command.Aliases...)
			}
			walk(command)
		}
	}
	walk(root)
	sort.Strings(got)

	want := []string{
		"completion",
		"config", "config edit", "config path", "config show",
		"doctor",
		"host", "host add", "host list", "host remove",
		"info", "net", "pull", "push", "run", "ssh", "storage",
		"theme", "theme list", "theme preview",
		"top", "tui", "version",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command inventory changed\n got: %v\nwant: %v", got, want)
	}

	wantAliases := map[string][]string{
		"host remove": {"rm"},
		"info":        {"neofetch", "fetch", "specs"},
		"net":         {"network", "bandwidth"},
		"storage":     {"disk", "du", "io"},
		"theme":       {"themes"},
		"top":         {"btop"},
		"tui":         {"ui", "dashboard"},
	}
	if !reflect.DeepEqual(aliases, wantAliases) {
		t.Fatalf("alias inventory changed\n got: %#v\nwant: %#v", aliases, wantAliases)
	}
}

func TestEveryCLICommandRendersHelp(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	paths := [][]string{
		{}, {"completion"},
		{"config"}, {"config", "edit"}, {"config", "path"}, {"config", "show"},
		{"doctor"},
		{"host"}, {"host", "add"}, {"host", "list"}, {"host", "remove"},
		{"info"}, {"net"}, {"pull"}, {"push"}, {"run"}, {"ssh"}, {"storage"},
		{"theme"}, {"theme", "list"}, {"theme", "preview"},
		{"top"}, {"tui"}, {"version"},
	}
	for _, path := range paths {
		args := append(append([]string(nil), path...), "--help")
		stdout, _, err := executeMatrixCommand(fixture.app(), args...)
		if err != nil {
			t.Fatalf("nexus %s: %v", strings.Join(args, " "), err)
		}
		if !strings.Contains(stdout, "Usage") {
			t.Fatalf("nexus %s produced no usage section:\n%s", strings.Join(args, " "), stdout)
		}
	}
}

func TestCLISuccessAndAliasMatrix(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	cases := [][]string{
		{},
		{"--version"}, {"version"},
		{"doctor"},
		{"config"}, {"config", "edit"}, {"config", "show"}, {"config", "path"},
		{"theme"}, {"themes"}, {"theme", "list"}, {"theme", "preview"},
		{"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"},
		{"host", "list"},
		{"host", "add", "bob@example.com:2200"},
		{"host", "add", "bob@example.com:2200"},
		{"host", "remove", "nobody@example.com"},
		{"host", "rm", "bob@example.com:2200"},
		{"ssh", "alice@example.com:2222"},
		{"top", "alice@example.com:2222"}, {"btop", "alice@example.com:2222"},
		{"net", "alice@example.com:2222"}, {"network", "alice@example.com:2222"}, {"bandwidth", "alice@example.com:2222"},
		{"info", "alice@example.com:2222"}, {"neofetch", "alice@example.com:2222"}, {"fetch", "alice@example.com:2222"}, {"specs", "alice@example.com:2222"},
		{"storage", "alice@example.com:2222"}, {"disk", "alice@example.com:2222"}, {"du", "alice@example.com:2222"}, {"io", "alice@example.com:2222"},
		{"pull", "alice@example.com:2222", "/remote/file", fixture.configDir},
		{"--dry-run", "pull", "alice@example.com:2222", "/remote/file", fixture.configDir},
		{"pull", "--dry-run", "/remote/file", "alice@example.com:2222", fixture.configDir},
		{"push", fixture.localFile, "alice@example.com:2222", "/remote"},
		{"--dry-run", "push", fixture.localFile, "alice@example.com:2222", "/remote"},
		{"push", fixture.localFile, "--dry-run", "alice@example.com:2222", "/remote"},
	}
	for _, args := range cases {
		if _, _, err := executeMatrixCommand(fixture.app(), args...); err != nil {
			t.Fatalf("nexus %s: %v", strings.Join(args, " "), err)
		}
	}
}

func TestCLIArgumentAndGlobalFlagFailureMatrix(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"unknown"}, "unknown command"},
		{[]string{"stray"}, "unknown command"},
		{[]string{"version", "extra"}, "unknown command"},
		{[]string{"completion"}, "accepts 1 arg"},
		{[]string{"completion", "tcsh"}, "unsupported shell"},
		{[]string{"completion", "bash", "extra"}, "accepts 1 arg"},
		{[]string{"config", "show", "extra"}, "unknown command"},
		{[]string{"config", "path", "extra"}, "unknown command"},
		{[]string{"config", "edit", "extra"}, "unknown command"},
		{[]string{"theme", "list", "extra"}, "unknown command"},
		{[]string{"theme", "preview", "extra"}, "unknown command"},
		{[]string{"host", "list", "extra"}, "unknown command"},
		{[]string{"host", "add"}, "accepts 1 arg"},
		{[]string{"host", "add", "a@example.com", "extra"}, "accepts 1 arg"},
		{[]string{"host", "add", "not-a-target"}, "invalid host format"},
		{[]string{"host", "remove"}, "accepts 1 arg"},
		{[]string{"ssh", "a@example.com", "extra"}, "accepts at most 1 arg"},
		{[]string{"ssh", "not-a-target"}, "invalid host"},
		{[]string{"top", "a@example.com", "extra"}, "accepts at most 1 arg"},
		{[]string{"net", "a@example.com", "extra"}, "accepts at most 1 arg"},
		{[]string{"info", "a@example.com", "extra"}, "accepts at most 1 arg"},
		{[]string{"storage", "a@example.com", "extra"}, "accepts at most 1 arg"},
		{[]string{"run"}, "accepts between 1 and 2 arg"},
		{[]string{"run", "one", "a@example.com", "extra"}, "accepts between 1 and 2 arg"},
		{[]string{"run", "missing", "alice@example.com:2222"}, "is not available"},
		{[]string{"pull", "a", "b", "c", "d"}, "accepts at most 3 arg"},
		{[]string{"push", "a", "b", "c", "d"}, "accepts at most 3 arg"},
		{[]string{"push", filepath.Join(fixture.configDir, "missing"), "alice@example.com"}, "local path does not exist"},
		{[]string{"--port=0", "ssh", "alice@example.com"}, "SSH port must be"},
		{[]string{"--port=-1", "ssh", "alice@example.com"}, "SSH port must be"},
		{[]string{"--port=65536", "ssh", "alice@example.com"}, "SSH port must be"},
		{[]string{"--indexing", "eager", "pull", "alice@example.com", "/x", fixture.configDir}, "indexing mode must be"},
		{[]string{"--not-a-flag"}, "unknown flag"},
		{[]string{"tui"}, "requires an interactive terminal"},
		{[]string{"ui"}, "requires an interactive terminal"},
		{[]string{"dashboard"}, "requires an interactive terminal"},
	}
	for _, tc := range cases {
		_, _, err := executeMatrixCommand(fixture.app(), tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("nexus %s error=%v, want substring %q", strings.Join(tc.args, " "), err, tc.want)
		}
	}
}

func TestCLIPersistentFlagPositionAndCombinationMatrix(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	cases := [][]string{
		{"--verbose", "--dry-run", "--indexing", "lazy", "--port", "2200", "pull", "alice@example.com", "/remote", fixture.configDir},
		{"pull", "alice@example.com", "/remote", fixture.configDir, "--verbose", "--dry-run", "--indexing", "full", "--port", "2201"},
		{"-v", "-n", "-i", "lazy", "-p", "2202", "push", fixture.localFile, "alice@example.com", "/remote"},
		{"push", fixture.localFile, "alice@example.com", "/remote", "-v", "-n", "-i", "full", "-p", "2203"},
	}
	for _, args := range cases {
		if _, _, err := executeMatrixCommand(fixture.app(), args...); err != nil {
			t.Fatalf("nexus %s: %v", strings.Join(args, " "), err)
		}
	}
	logged, err := os.ReadFile(fixture.logFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"2200", "2201", "2202", "2203"} {
		if !strings.Contains(string(logged), "-p "+port) {
			t.Fatalf("combined flag matrix did not propagate port %s:\n%s", port, logged)
		}
	}
}

func TestCLIBootstrapFailureAndSkipMatrix(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	if err := os.WriteFile(fixture.configFile, []byte("unknown_setting: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executeMatrixCommand(fixture.app(), "host", "list"); err == nil || !strings.Contains(err.Error(), "field unknown_setting") {
		t.Fatalf("bootstrap command accepted invalid config: %v", err)
	}
	for _, args := range [][]string{{"--version"}, {"version"}, {"completion", "bash"}} {
		if _, _, err := executeMatrixCommand(fixture.app(), args...); err != nil {
			t.Fatalf("bootstrap-independent nexus %s failed: %v", strings.Join(args, " "), err)
		}
	}
}

func TestCLIOutputContracts(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"--version"}, []string{"nexus dev"}},
		{[]string{"version"}, []string{"nexus dev", runtimePlatform()}},
		{[]string{"doctor"}, []string{"ssh", "rsync", "fzf", "ssh-copy-id", "config", "history", "state", "theme"}},
		{[]string{"config", "path"}, []string{fixture.configFile}},
		{[]string{"config", "show"}, []string{"theme: nexus", "name: uptime"}},
		{[]string{"theme", "list"}, []string{"catppuccin", "dracula", "gruvbox", "mono", "nexus", "nord", "terminal"}},
		{[]string{"completion", "bash"}, []string{"__start_nexus"}},
		{[]string{"completion", "zsh"}, []string{"#compdef nexus"}},
		{[]string{"completion", "fish"}, []string{"complete -c nexus"}},
		{[]string{"completion", "powershell"}, []string{"Register-ArgumentCompleter"}},
	}
	for _, tc := range cases {
		stdout, _, err := executeMatrixCommand(fixture.app(), tc.args...)
		if err != nil {
			t.Fatalf("nexus %s: %v", strings.Join(tc.args, " "), err)
		}
		for _, want := range tc.want {
			if !strings.Contains(stdout, want) {
				t.Fatalf("nexus %s output missing %q:\n%s", strings.Join(tc.args, " "), want, stdout)
			}
		}
	}
}

func TestCLIDependencyFailureMatrix(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	emptyBin := filepath.Join(fixture.configDir, "empty-bin")
	if err := os.MkdirAll(emptyBin, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", emptyBin)
	if _, _, err := executeMatrixCommand(fixture.app(), "doctor"); err == nil || !strings.Contains(err.Error(), "required dependency missing: ssh") {
		t.Fatalf("doctor without ssh error=%v", err)
	}
	if _, _, err := executeMatrixCommand(fixture.app(), "ssh", "alice@example.com"); err == nil || !strings.Contains(err.Error(), "ssh not found") {
		t.Fatalf("ssh without ssh executable error=%v", err)
	}
	if _, _, err := executeMatrixCommand(fixture.app(), "info", "alice@example.com"); err == nil || !strings.Contains(err.Error(), "ssh not found") {
		t.Fatalf("info without ssh executable error=%v", err)
	}

	t.Setenv("VISUAL", "missing-editor")
	t.Setenv("EDITOR", "missing-editor")
	if _, _, err := executeMatrixCommand(fixture.app(), "config", "edit"); err == nil || !strings.Contains(err.Error(), "no suitable editor") {
		t.Fatalf("config edit without editor error=%v", err)
	}
}

func TestCLITransportFailureAndCancellationMatrix(t *testing.T) {
	fixture := newCLIMatrixFixture(t)
	binDir := filepath.Dir(os.Getenv("NEXUS_RSYNC_PATH"))

	writeExecutable(t, filepath.Join(binDir, "ssh"), "#!/bin/sh\nexit 255\n")
	if _, _, err := executeMatrixCommand(fixture.app(), "ssh", "alice@example.com"); err == nil || !strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("SSH transport failure error=%v", err)
	}
	if _, _, err := executeMatrixCommand(fixture.app(), "net", "alice@example.com"); err == nil || !strings.Contains(err.Error(), "remote command probe failed") {
		t.Fatalf("diagnostic transport failure error=%v", err)
	}

	writeExecutable(t, filepath.Join(binDir, "rsync"), "#!/bin/sh\nexit 12\n")
	t.Setenv("NEXUS_RSYNC_PATH", filepath.Join(binDir, "rsync"))
	if _, _, err := executeMatrixCommand(fixture.app(), "pull", "alice@example.com", "/remote", fixture.configDir); err == nil ||
		!strings.Contains(err.Error(), "rsync") || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("rsync transport failure error=%v", err)
	}

	writeExecutable(t, filepath.Join(binDir, "fzf"), "#!/bin/sh\nexit 1\n")
	for _, args := range [][]string{{"ssh"}, {"pull"}, {"run", "uptime"}} {
		if _, _, err := executeMatrixCommand(fixture.app(), args...); err != nil {
			t.Fatalf("cancelled nexus %s returned error: %v", strings.Join(args, " "), err)
		}
	}
}

func runtimePlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func newCLIMatrixFixture(t *testing.T) cliMatrixFixture {
	t.Helper()
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(tempDir, "commands.log")
	writeExecutable(t, filepath.Join(binDir, "ssh"), `#!/bin/sh
printf '%s\n' "$*" >> "$NEXUS_FAKE_LOG"
case "$*" in
  *"OS="*"TOOLS="*) printf 'OS=Test Linux\nCPU=Test CPU\nMEMORY_BYTES=17179869184\nGPU=Test GPU\nDISK=/dev/root\t/\t5000000000\t15000000000\t20000000000\nTOOLS=top,df\n'; exit 0 ;;
  *"sudo -n true"*) exit 1 ;;
  *"for cmd in "*"fastfetch"*) exit 127 ;;
  *"for cmd in "*"iftop"*) printf 'ip\n'; exit 0 ;;
  *"for cmd in "*"btop"*) printf 'top\n'; exit 0 ;;
  *"for cmd in "*"duf"*) printf 'df\n'; exit 0 ;;
esac
printf 'REMOTE_OK\n'
`)
	writeExecutable(t, filepath.Join(binDir, "rsync"), `#!/bin/sh
printf '%s\n' "$*" >> "$NEXUS_FAKE_LOG"
if [ "$1" = "--version" ]; then printf 'rsync version 3.2.7 protocol version 31\n'; fi
`)
	writeExecutable(t, filepath.Join(binDir, "fzf"), "#!/bin/sh\nsed -n '1p'\n")
	writeExecutable(t, filepath.Join(binDir, "editor"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NEXUS_RSYNC_PATH", filepath.Join(binDir, "rsync"))
	t.Setenv("NEXUS_FAKE_LOG", logFile)
	t.Setenv("VISUAL", filepath.Join(binDir, "editor"))
	t.Setenv("EDITOR", filepath.Join(binDir, "editor"))

	fixture := cliMatrixFixture{
		configDir:  filepath.Join(tempDir, "config"),
		configFile: filepath.Join(tempDir, "config", "config.yaml"),
		hostsFile:  filepath.Join(tempDir, "config", "hosts.json"),
		stateFile:  filepath.Join(tempDir, "config", "state.json"),
		localFile:  filepath.Join(tempDir, "payload.txt"),
		logFile:    logFile,
	}
	if err := fixture.app().ensureBootstrap(); err != nil {
		t.Fatal(err)
	}
	config := `ui:
  theme: nexus
commands:
  - name: uptime
    description: Show uptime
    command: uptime
`
	if err := os.WriteFile(fixture.configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.hostsFile, []byte("[\"alice@example.com:2222\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.localFile, []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f cliMatrixFixture) app() *app {
	return &app{
		configDir:  f.configDir,
		configFile: f.configFile,
		hostsFile:  f.hostsFile,
		stateFile:  f.stateFile,
	}
}

func executeMatrixCommand(a *app, args ...string) (string, string, error) {
	root := a.newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}
