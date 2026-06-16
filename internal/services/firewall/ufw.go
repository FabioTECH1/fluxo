package firewall

import (
	"context"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// ruleAction returns the UFW action string, defaulting to "allow".
func ruleAction(ruleType string) string {
	if strings.EqualFold(ruleType, "deny") {
		return "deny"
	}
	return "allow"
}

// AddRule creates a UFW rule for the given port and optional source IP.
func AddRule(port, fromIP, ruleType string) error {
	fromIP = strings.TrimSpace(fromIP)
	port = strings.TrimSpace(port)
	action := ruleAction(ruleType)

	ctx := context.Background()

	if fromIP == "" || strings.EqualFold(fromIP, "Any") {
		_, err := syscmd.Run(ctx, 10*time.Second, "ufw", action, port)
		return err
	}

	_, err := syscmd.Run(ctx, 10*time.Second, "ufw", action, "from", fromIP, "to", "any", "port", port)
	return err
}

// DeleteRule removes a UFW rule for the given port and optional source IP.
func DeleteRule(port, fromIP, ruleType string) error {
	fromIP = strings.TrimSpace(fromIP)
	port = strings.TrimSpace(port)
	action := ruleAction(ruleType)

	ctx := context.Background()

	if fromIP == "" || strings.EqualFold(fromIP, "Any") {
		_, err := syscmd.Run(ctx, 10*time.Second, "ufw", "delete", action, port)
		return err
	}

	_, err := syscmd.Run(ctx, 10*time.Second, "ufw", "delete", action, "from", fromIP, "to", "any", "port", port)
	return err
}

// Status returns the numbered UFW status output.
func Status() (string, error) {
	output, err := syscmd.Run(context.Background(), 5*time.Second, "ufw", "status", "numbered")
	return output, err
}
