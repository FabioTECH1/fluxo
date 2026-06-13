package firewall

import (
	"context"
	"strings"
	"time"

	"fluxo/syscmd"
)

func ruleAction(ruleType string) string {
	if strings.EqualFold(ruleType, "deny") {
		return "deny"
	}
	return "allow"
}

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

func Status() (string, error) {
	output, err := syscmd.Run(context.Background(), 5*time.Second, "ufw", "status", "numbered")
	return output, err
}
