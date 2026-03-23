package cmd

import (
	"errors"
	"fmt"

	"nexus/internal/hosts"
	"nexus/internal/ui"
)

func mustStore() (*hosts.Store, error) {
	store, err := hosts.NewDefaultStore()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize host store: %w", err)
	}
	return store, nil
}

func selectKnownHost(store *hosts.Store) (string, error) {
	knownHosts, err := store.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load hosts: %w", err)
	}
	if len(knownHosts) == 0 {
		return "", errors.New("no hosts in history; add one with `nexus host add user@ip` or use `nexus ssh` to add interactively")
	}

	host, err := ui.Select("host> ", knownHosts)
	if errors.Is(err, ui.ErrNoSelection) {
		return "", ui.ErrNoSelection
	}
	if err != nil {
		return "", fmt.Errorf("host selection failed: %w", err)
	}
	return host, nil
}
