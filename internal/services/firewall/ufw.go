package firewall

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/safeinput"
	"fluxo/internal/syscmd"
)

// ruleAction returns the UFW action string, defaulting to "allow".
func ruleAction(ruleType string) string {
	if strings.EqualFold(ruleType, "deny") {
		return "deny"
	}
	return "allow"
}

func ruleCommandArgs(port, fromIP, ruleType string) []string {
	action := ruleAction(ruleType)
	if fromIP == "" || strings.EqualFold(fromIP, "Any") || strings.EqualFold(fromIP, "Anywhere") {
		return []string{action, port}
	}
	portNumber := port
	protocol := ""
	if parts := strings.Split(port, "/"); len(parts) == 2 {
		portNumber = parts[0]
		protocol = parts[1]
	}
	args := []string{action, "from", fromIP, "to", "any", "port", portNumber}
	if protocol != "" {
		args = append(args, "proto", protocol)
	}
	return args
}

// NormalizeSource converts equivalent UFW source forms into one persisted value.
func NormalizeSource(fromIP string) string {
	fromIP = strings.TrimSpace(fromIP)
	if fromIP == "" || strings.EqualFold(fromIP, "Any") || strings.EqualFold(fromIP, "Anywhere") {
		return "Any"
	}
	if ip := net.ParseIP(fromIP); ip != nil {
		return ip.String()
	}
	if _, network, err := net.ParseCIDR(fromIP); err == nil {
		return network.String()
	}
	return fromIP
}

// NormalizePort removes insignificant numeric formatting while preserving protocol and profiles.
func NormalizePort(port string) string {
	port = strings.TrimSpace(port)
	protocol := ""
	base := port
	if parts := strings.Split(port, "/"); len(parts) == 2 {
		base = parts[0]
		protocol = "/" + parts[1]
	}
	if parts := strings.Split(base, ":"); len(parts) == 2 {
		start, startErr := strconv.Atoi(parts[0])
		end, endErr := strconv.Atoi(parts[1])
		if startErr == nil && endErr == nil {
			return fmt.Sprintf("%d:%d%s", start, end, protocol)
		}
	}
	if number, err := strconv.Atoi(base); err == nil {
		return fmt.Sprintf("%d%s", number, protocol)
	}
	return port
}

func canonicalAddedRule(port, fromIP, ruleType string) string {
	args := ruleCommandArgs(NormalizePort(port), NormalizeSource(fromIP), strings.TrimSpace(ruleType))
	for i, arg := range args {
		if strings.Contains(arg, " ") {
			args[i] = "'" + arg + "'"
		}
	}
	return "ufw " + strings.Join(args, " ")
}

// AddRule creates a UFW rule for the given port and optional source IP.
func AddRule(port, fromIP, ruleType string) error {
	fromIP = strings.TrimSpace(fromIP)
	port = strings.TrimSpace(port)
	ruleType = strings.TrimSpace(ruleType)
	if !safeinput.ValidateFirewallAction(ruleType) {
		return fmt.Errorf("invalid firewall action")
	}
	if !safeinput.ValidateFirewallPortSpec(port) {
		return fmt.Errorf("invalid firewall port")
	}
	if !safeinput.ValidateFirewallSource(fromIP) {
		return fmt.Errorf("invalid firewall source")
	}
	port = NormalizePort(port)
	fromIP = NormalizeSource(fromIP)
	args := ruleCommandArgs(port, fromIP, ruleType)
	_, err := syscmd.Run(context.Background(), 10*time.Second, "ufw", args...)
	return err
}

// DeleteRule removes a UFW rule for the given port and optional source IP.
func DeleteRule(port, fromIP, ruleType string) error {
	fromIP = strings.TrimSpace(fromIP)
	port = strings.TrimSpace(port)
	ruleType = strings.TrimSpace(ruleType)
	if !safeinput.ValidateFirewallAction(ruleType) || !safeinput.ValidateFirewallPortSpec(port) || !safeinput.ValidateFirewallSource(fromIP) {
		return fmt.Errorf("invalid firewall rule")
	}
	port = NormalizePort(port)
	fromIP = NormalizeSource(fromIP)
	args := append([]string{"delete"}, ruleCommandArgs(port, fromIP, ruleType)...)
	_, err := syscmd.Run(context.Background(), 10*time.Second, "ufw", args...)
	return err
}

// AddedRules returns UFW's persisted rule commands, including inactive policies.
func AddedRules() (string, error) {
	return syscmd.Run(context.Background(), 5*time.Second, "ufw", "show", "added")
}

// RuleExists reports whether a managed rule still exists in UFW's persisted rules.
func RuleExists(addedRules, port, fromIP, ruleType string) bool {
	expected := canonicalAddedRule(port, fromIP, ruleType)
	for _, line := range strings.Split(addedRules, "\n") {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}

// Status returns the numbered UFW status output.
func Status() (string, error) {
	output, err := syscmd.Run(context.Background(), 5*time.Second, "ufw", "status", "numbered")
	return output, err
}
