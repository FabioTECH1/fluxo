package site

import "strings"

func NormalizeAppType(appType string) string {
	return strings.ToLower(strings.TrimSpace(appType))
}

func IsValidAppType(appType string) bool {
	switch NormalizeAppType(appType) {
	case "php", "laravel", "html", "node", "wordpress", "python":
		return true
	default:
		return false
	}
}

func UsesPHP(appType string) bool {
	switch NormalizeAppType(appType) {
	case "php", "laravel", "wordpress":
		return true
	default:
		return false
	}
}

func SupportsDatabase(appType string) bool {
	switch NormalizeAppType(appType) {
	case "php", "laravel", "wordpress", "python":
		return true
	default:
		return false
	}
}

func UsesReverseProxy(appType string) bool {
	switch NormalizeAppType(appType) {
	case "node", "python":
		return true
	default:
		return false
	}
}

func SupportsZeroDowntime(appType string) bool {
	switch NormalizeAppType(appType) {
	case "php", "laravel", "html", "node", "python":
		return true
	default:
		return false
	}
}
