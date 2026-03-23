package hosts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	appConfigDir = "nexus"
	fileName     = "hosts.json"
)

var (
	userPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	hostPattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*)$`)
)

type payload struct {
	Hosts []string `json:"hosts"`
}

type Store struct {
	path string
}

func NewDefaultStore() (*Store, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve user home directory: %w", err)
	}
	return &Store{
		path: filepath.Join(homeDir, ".config", appConfigDir, fileName),
	}, nil
}

func (s *Store) Load() ([]string, error) {
	content, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", s.path, err)
	}

	content = bytesTrimSpace(content)
	if len(content) == 0 {
		return []string{}, nil
	}

	var hosts []string
	if content[0] == '[' {
		if err := json.Unmarshal(content, &hosts); err != nil {
			return nil, fmt.Errorf("invalid hosts array in %s: %w", s.path, err)
		}
		return unique(hosts), nil
	}

	var data payload
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("invalid hosts payload in %s: %w", s.path, err)
	}
	return unique(data.Hosts), nil
}

func (s *Store) Save(hosts []string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data := payload{Hosts: unique(hosts)}
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode hosts json: %w", err)
	}
	content = append(content, '\n')

	if err := os.WriteFile(s.path, content, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", s.path, err)
	}
	return nil
}

func (s *Store) Add(host string) (bool, error) {
	if err := Validate(host); err != nil {
		return false, err
	}

	current, err := s.Load()
	if err != nil {
		return false, err
	}
	for _, item := range current {
		if item == host {
			return false, nil
		}
	}

	current = append(current, host)
	return true, s.Save(current)
}

func (s *Store) Remove(host string) (bool, error) {
	current, err := s.Load()
	if err != nil {
		return false, err
	}

	filtered := make([]string, 0, len(current))
	removed := false
	for _, item := range current {
		if item == host {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}

	if !removed {
		return false, nil
	}
	return true, s.Save(filtered)
}

func Validate(host string) error {
	if strings.Count(host, "@") != 1 {
		return errors.New("expected exactly one '@' in user@ip format")
	}

	parts := strings.SplitN(host, "@", 2)
	user := strings.TrimSpace(parts[0])
	hostname := strings.TrimSpace(parts[1])
	if user == "" || hostname == "" {
		return errors.New("user and host must both be non-empty")
	}

	if !userPattern.MatchString(user) {
		return errors.New("user contains unsupported characters")
	}

	if strings.HasPrefix(hostname, "[") && strings.HasSuffix(hostname, "]") {
		hostname = strings.TrimPrefix(strings.TrimSuffix(hostname, "]"), "[")
	}

	if net.ParseIP(hostname) != nil {
		return nil
	}
	if hostPattern.MatchString(hostname) {
		return nil
	}
	return errors.New("host must be a valid IP address or hostname")
}

func unique(items []string) []string {
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

func bytesTrimSpace(input []byte) []byte {
	start := 0
	for start < len(input) && isSpace(input[start]) {
		start++
	}
	end := len(input)
	for end > start && isSpace(input[end-1]) {
		end--
	}
	return input[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}
