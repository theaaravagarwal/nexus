package nexus

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

const defaultSSHPort = 22

// connectionTarget is the single parsed representation used by SSH, rsync,
// history, and host-profile lookup. Keeping the port separate from the SSH
// destination prevents user input from being interpolated into shell strings.
type connectionTarget struct {
	User string
	Host string
	Port int
}

func parseConnectionTarget(raw string) (connectionTarget, error) {
	raw = strings.TrimSpace(raw)
	if strings.Count(raw, "@") != 1 {
		return connectionTarget{}, validationErr("expected exactly one '@' in user@host format")
	}

	user, address, _ := strings.Cut(raw, "@")
	user = strings.TrimSpace(user)
	address = strings.TrimSpace(address)
	if user == "" || address == "" {
		return connectionTarget{}, validationErr("user and host must both be non-empty")
	}
	if !userPattern.MatchString(user) {
		return connectionTarget{}, validationErr("username contains unsupported characters")
	}

	host, port, err := splitHostPort(address)
	if err != nil {
		return connectionTarget{}, err
	}
	if net.ParseIP(host) == nil && !hostPattern.MatchString(host) {
		return connectionTarget{}, validationErr("host must be a valid IP address or hostname")
	}

	return connectionTarget{User: user, Host: host, Port: port}, nil
}

func splitHostPort(address string) (string, int, error) {
	if strings.HasPrefix(address, "[") {
		closeBracket := strings.IndexByte(address, ']')
		if closeBracket < 0 {
			return "", 0, validationErr("missing closing bracket in IPv6 host")
		}
		host := strings.TrimSpace(address[1:closeBracket])
		suffix := strings.TrimSpace(address[closeBracket+1:])
		if net.ParseIP(host) == nil {
			return "", 0, validationErr("brackets are only valid around an IP address")
		}
		if suffix == "" {
			return host, defaultSSHPort, nil
		}
		if !strings.HasPrefix(suffix, ":") || strings.Count(suffix, ":") != 1 {
			return "", 0, validationErr("invalid host suffix; expected :port")
		}
		port, err := parseSSHPort(strings.TrimPrefix(suffix, ":"))
		return host, port, err
	}

	if ip := net.ParseIP(address); ip != nil {
		return address, defaultSSHPort, nil
	}
	if strings.Count(address, ":") > 1 {
		return "", 0, validationErr("IPv6 addresses with ports must use [address]:port")
	}
	if host, rawPort, ok := strings.Cut(address, ":"); ok {
		port, err := parseSSHPort(rawPort)
		if err != nil {
			return "", 0, err
		}
		return strings.TrimSpace(host), port, nil
	}
	return address, defaultSSHPort, nil
}

func parseSSHPort(raw string) (int, error) {
	if raw == "" {
		return 0, validationErr("SSH port is empty")
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, validationErr("SSH port must be a number from 1 to 65535")
		}
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, validationErr("SSH port must be a number from 1 to 65535")
	}
	return port, nil
}

func canonicalConnectionTarget(raw string, portOverride int) (connectionTarget, error) {
	target, err := parseConnectionTarget(raw)
	if err != nil {
		return connectionTarget{}, err
	}
	if portOverride != 0 {
		if portOverride < 1 || portOverride > 65535 {
			return connectionTarget{}, validationErr("SSH port must be a number from 1 to 65535")
		}
		target.Port = portOverride
	}
	return target, nil
}

func (t connectionTarget) String() string {
	host := t.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	base := t.User + "@" + host
	if t.Port != defaultSSHPort {
		return fmt.Sprintf("%s:%d", base, t.Port)
	}
	return base
}

func (t connectionTarget) sshDestination() string {
	return t.User + "@" + t.Host
}

func (t connectionTarget) rsyncDestination() string {
	host := t.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return t.User + "@" + host
}
