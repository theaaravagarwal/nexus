//go:build windows

package app

import "os"

func checkFileOwnership(string, os.FileInfo) error { return nil }
