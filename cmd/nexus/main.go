package main

import (
	"fmt"
	"os"

	"nexus/internal/nexus"
)

func main() {
	if err := nexus.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
