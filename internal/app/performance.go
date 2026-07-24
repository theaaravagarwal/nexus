package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	controlPathOnce sync.Once
	controlPath     string
	rsyncVersionRE  = regexp.MustCompile(`(?i)version\s+(\d+)\.(\d+)(?:\.(\d+))?`)
	rsyncSkipCache  sync.Map
)

func sshMultiplexArgs() []string {
	path := nexusControlPath()
	if path == "" {
		return nil
	}
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=120",
		"-o", "ControlPath=" + path,
	}
}

func nexusControlPath() string {
	if os.PathSeparator == '\\' {
		return ""
	}
	controlPathOnce.Do(func() {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			return
		}
		dir := filepath.Join(cacheDir, "nexus", "ssh")
		if err := ensurePrivateDirectory(dir); err != nil {
			return
		}
		candidate := filepath.Join(dir, "mux-%C")
		// OpenSSH control sockets have a small platform-dependent path limit.
		// Disable reuse instead of creating a socket name that will fail later.
		if len(candidate) > 95 || strings.ContainsAny(candidate, " \t\n") {
			return
		}
		controlPath = candidate
	})
	return controlPath
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
			return os.ErrPermission
		}
		return nil
	case !os.IsNotExist(err):
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err = os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return os.ErrPermission
	}
	return nil
}

func rsyncSupportsSkipCompress(binary string) bool {
	if cached, ok := rsyncSkipCache.Load(binary); ok {
		return cached.(bool)
	}
	cmd := exec.Command(binary, "--version")
	var output bytes.Buffer
	cmd.Stdout = &output
	supported := cmd.Run() == nil && rsyncVersionSupportsSkipCompress(output.String())
	rsyncSkipCache.Store(binary, supported)
	return supported
}

func rsyncVersionSupportsSkipCompress(output string) bool {
	match := rsyncVersionRE.FindStringSubmatch(output)
	if len(match) < 3 {
		return false
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	return major > 3 || major == 3 && minor >= 1
}

func appendSkipCompress(args []string, binary string) []string {
	if !rsyncSupportsSkipCompress(binary) {
		return args
	}
	return append(args, "--skip-compress="+strings.Join([]string{
		"7z", "avi", "bz2", "deb", "gz", "iso", "jar", "jpeg", "jpg", "mkv",
		"mov", "mp3", "mp4", "ogg", "png", "rar", "rpm", "tbz", "tgz", "wim",
		"xz", "zip", "zst",
	}, "/"))
}
