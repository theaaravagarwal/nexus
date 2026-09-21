//go:build unix

package app

import (
	"fmt"
	"os"
	"syscall"
)

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
