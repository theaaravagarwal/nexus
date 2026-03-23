package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func ExpandUser(path string) (string, error) {
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

// NormalizeForRsync converts Windows paths to MSYS2-compatible format.
func NormalizeForRsync(path string) string {
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
