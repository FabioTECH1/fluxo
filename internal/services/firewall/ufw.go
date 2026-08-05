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

// AddedRule is a persisted UFW command parsed from `ufw show added`. External
// rules are exposed read-only, so the original command is retained for display.
type AddedRule struct {
	RuleType          string
	Port              string
	FromIP            string
	Command           string
	ManagedEquivalent bool
}

func (rule AddedRule) Matches(port, fromIP, ruleType string) bool {
	return rule.ManagedEquivalent &&
		strings.EqualFold(rule.RuleType, strings.TrimSpace(ruleType)) &&
		NormalizePort(rule.Port) == NormalizePort(port) &&
		NormalizeSource(rule.FromIP) == NormalizeSource(fromIP)
}

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

// ParseAddedRules converts the common rule forms emitted by `ufw show added`
// into display-safe structured values. Unsupported options remain visible as a
// custom rule with their original command instead of being treated as managed.
func ParseAddedRules(output string) []AddedRule {
	rules := make([]AddedRule, 0)
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "ufw ") {
			continue
		}
		tokens, ok := splitCommandFields(line)
		if !ok || len(tokens) < 3 || tokens[0] != "ufw" {
			continue
		}
		actionIndex := 1
		routed := false
		if tokens[actionIndex] == "route" {
			routed = true
			actionIndex++
		}
		if actionIndex >= len(tokens) {
			continue
		}
		action := strings.ToLower(tokens[actionIndex])
		if action != "allow" && action != "deny" && action != "reject" && action != "limit" {
			continue
		}

		rule := AddedRule{RuleType: action, Port: "Custom rule", FromIP: "Any", Command: line}
		args := tokens[actionIndex+1:]
		for index := 0; index < len(args); index++ {
			switch args[index] {
			case "from":
				if index+1 < len(args) {
					rule.FromIP = NormalizeSource(args[index+1])
				}
			case "port", "app":
				if index+1 < len(args) {
					rule.Port = NormalizePort(args[index+1])
				}
			}
		}
		if len(args) > 0 && !isUFWRuleKeyword(args[0]) {
			rule.Port = NormalizePort(args[0])
		}
		if protocol := optionValue(args, "proto"); protocol != "" && rule.Port != "Custom rule" && !strings.Contains(rule.Port, "/") {
			rule.Port += "/" + strings.ToLower(protocol)
		}
		rule.ManagedEquivalent = managedEquivalentRule(routed, action, args, rule)
		rules = append(rules, rule)
	}
	return rules
}

func managedEquivalentRule(routed bool, action string, args []string, rule AddedRule) bool {
	if routed || (action != "allow" && action != "deny") ||
		!safeinput.ValidateFirewallPortSpec(rule.Port) ||
		!safeinput.ValidateFirewallSource(rule.FromIP) {
		return false
	}
	for _, arg := range args {
		if arg == "in" || arg == "out" || arg == "on" || arg == "log" {
			return false
		}
	}
	from := optionValue(args, "from")
	to := optionValue(args, "to")
	if from != "" {
		return strings.EqualFold(to, "any") && (optionValue(args, "port") != "" || optionValue(args, "app") != "")
	}
	return len(args) > 0 && !isUFWRuleKeyword(args[0])
}

func optionValue(args []string, name string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name {
			return args[index+1]
		}
	}
	return ""
}

func isUFWRuleKeyword(value string) bool {
	switch value {
	case "in", "out", "on", "from", "to", "port", "proto", "app", "comment", "log":
		return true
	default:
		return strings.HasPrefix(value, "--")
	}
}

func splitCommandFields(command string) ([]string, bool) {
	fields := make([]string, 0)
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			fields = append(fields, current.String())
			current.Reset()
		}
	}
	for _, char := range command {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == ' ' || char == '\t' {
			flush()
			continue
		}
		current.WriteRune(char)
	}
	if quote != 0 || escaped {
		return nil, false
	}
	flush()
	return fields, true
}

// RuleExists reports whether a managed rule still exists in UFW's persisted rules.
func RuleExists(addedRules, port, fromIP, ruleType string) bool {
	for _, rule := range ParseAddedRules(addedRules) {
		if rule.Matches(port, fromIP, ruleType) {
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
