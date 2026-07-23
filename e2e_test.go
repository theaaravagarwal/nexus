package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIEndToEndWithPortableRemote(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(tempDir, "commands.log")
	writeExecutable(t, filepath.Join(binDir, "ssh"), `#!/bin/sh
printf '%s\n' "$*" >> "$NEXUS_FAKE_LOG"
case "$*" in
  *"OS="*"TOOLS="*)
    printf 'OS=Test Linux\nCPU=Test CPU\nMEMORY=16 GiB\nDISK=5 / 20 GiB\nTOOLS=top,df\n'
    exit 0
    ;;
  *"sudo -n true"*) exit 1 ;;
  *"for cmd in "*"fastfetch"*) exit 127 ;;
  *"for cmd in "*"iftop"*)
    if [ "${NEXUS_FAKE_NO_NET_TOOLS:-0}" -eq 1 ]; then exit 127; fi
    printf 'ip\n'
    exit 0
    ;;
  *"for cmd in "*"btop"*) printf 'top\n'; exit 0 ;;
  *"for cmd in "*"duf"*) printf 'df\n'; exit 0 ;;
esac
if [ "${NEXUS_FAKE_SSH_EXIT:-0}" -ne 0 ]; then
  exit "$NEXUS_FAKE_SSH_EXIT"
fi
printf 'REMOTE_OK\n'
`)
	writeExecutable(t, filepath.Join(binDir, "rsync"), `#!/bin/sh
printf '%s\n' "$*" >> "$NEXUS_FAKE_LOG"
case "$1" in
  --version) printf 'rsync version 3.2.7 protocol version 31\n' ;;
esac
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "fzf"), "#!/bin/sh\nsed -n '1p'\n")
	writeExecutable(t, filepath.Join(binDir, "editor"), `#!/bin/sh
printf 'editor %s\n' "$*" >> "$NEXUS_FAKE_LOG"
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NEXUS_RSYNC_PATH", filepath.Join(binDir, "rsync"))
	t.Setenv("NEXUS_FAKE_LOG", logPath)
	t.Setenv("VISUAL", filepath.Join(binDir, "editor"))
	t.Setenv("EDITOR", filepath.Join(binDir, "editor"))

	app := &app{
		configDir:  filepath.Join(tempDir, "config"),
		configFile: filepath.Join(tempDir, "config", "config.yaml"),
		hostsFile:  filepath.Join(tempDir, "config", "hosts.json"),
		stateFile:  filepath.Join(tempDir, "config", "state.json"),
	}
	if err := app.ensureBootstrap(); err != nil {
		t.Fatal(err)
	}
	config := `ui:
  theme: nexus
commands:
  - name: uptime
    description: Show uptime
    command: uptime
host_profiles:
  alice@example.com:2222:
    alias: lab
    tags: [test]
`
	if err := os.WriteFile(app.configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.hostsFile, []byte("[\"alice@example.com:2222\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"--version"},
		{"version"},
		{"doctor"},
		{"config", "show"},
		{"config", "path"},
		{"config"},
		{"config", "edit"},
		{"theme", "list"},
		{"theme", "preview"},
		{"completion", "bash"},
		{"completion", "zsh"},
		{"completion", "fish"},
		{"completion", "powershell"},
		{"host", "list"},
		{"info", "alice@example.com:2222"},
		{"net", "alice@example.com:2222"},
		{"storage", "alice@example.com:2222"},
		{"top", "alice@example.com:2222"},
	} {
		runRootCommand(t, app, args...)
	}
	t.Setenv("NEXUS_FAKE_NO_NET_TOOLS", "1")
	runRootCommand(t, app, "net", "alice@example.com:2222")
	t.Setenv("NEXUS_FAKE_NO_NET_TOOLS", "0")

	t.Setenv("NEXUS_FAKE_SSH_EXIT", "1")
	runRootCommand(t, app, "ssh", "alice@example.com:2222")
	t.Setenv("NEXUS_FAKE_SSH_EXIT", "0")
	runRootCommand(t, app, "--port", "2200", "ssh", "alice@example.com")

	localFile := filepath.Join(tempDir, "payload.txt")
	if err := os.WriteFile(localFile, []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runRootCommand(t, app, "--dry-run", "pull", "alice@example.com:2222", "/remote/file", tempDir)
	runRootCommand(t, app, "--dry-run", "push", localFile, "alice@example.com:2222", "/remote/upload")
	runRootCommand(t, app, "--dry-run", "--indexing", "full", "pull")
	runRootCommand(t, app, "--dry-run", "push")

	beforeDecline, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	runRootCommandWithInput(t, app, "n\n", "run", "uptime", "alice@example.com:2222")
	afterDecline, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterDecline) != string(beforeDecline) {
		t.Fatalf("declined custom command invoked a remote command:\nbefore:\n%s\nafter:\n%s", beforeDecline, afterDecline)
	}
	runRootCommandWithInput(t, app, "y\n", "run", "uptime", "alice@example.com:2222")

	runRootCommand(t, app, "host", "add", "bob@example.com:2200")
	runRootCommand(t, app, "host", "remove", "bob@example.com:2200")

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logged)
	for _, want := range []string{
		"-p 2222",
		"-p 2200",
		"editor " + app.configFile,
		"exec ip -s link",
		"exec df -h",
		"exec top",
		"uptime",
		"alice@example.com:/remote/file",
		"alice@example.com:/remote/upload",
		"-maxdepth 5",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("end-to-end command log missing %q:\n%s", want, logText)
		}
	}
}

func runRootCommand(t *testing.T, app *app, args ...string) {
	t.Helper()
	root := app.newRootCmd()
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("nexus %s: %v", strings.Join(args, " "), err)
	}
}

func runRootCommandWithInput(t *testing.T, app *app, input string, args ...string) {
	t.Helper()
	previousStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = reader
	defer func() {
		os.Stdin = previousStdin
		_ = reader.Close()
	}()
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	runRootCommand(t, app, args...)
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}
