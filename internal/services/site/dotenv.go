package site

import (
	"sort"
	"strings"
)

// quoteDotEnvValue preserves spaces and dotenv comment/interpolation markers.
// Site creation validation rejects single quotes for dotenv-based apps.
func quoteDotEnvValue(value string) string {
	return "'" + value + "'"
}

// databaseDotEnvReplacements returns the complete database configuration used
// by dotenv-based applications. Passwords are always quoted so characters such
// as # and $ remain literal when the application parses its environment.
func databaseDotEnvReplacements(req ProvisionRequest) map[string]string {
	dbConnection := "mysql"
	dbPort := "3306"
	if strings.EqualFold(req.DatabaseEngine, "postgres") || strings.EqualFold(req.DatabaseEngine, "pgsql") {
		dbConnection = "pgsql"
		dbPort = "5432"
	}

	return map[string]string{
		"DB_CONNECTION": dbConnection,
		"DB_HOST":       "127.0.0.1",
		"DB_PORT":       dbPort,
		"DB_DATABASE":   req.DatabaseName,
		"DB_USERNAME":   req.DatabaseUser,
		"DB_PASSWORD":   quoteDotEnvValue(req.DatabasePassword),
	}
}

func mergeDotEnvValues(content string, replacements map[string]string) string {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	lines := dotEnvStatements(content, newline)
	databaseKeys := []string{"DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD"}
	grouped := make(map[string]bool)
	var databaseLines []string
	if _, ok := replacements["DB_CONNECTION"]; ok {
		for _, key := range databaseKeys {
			if value, exists := replacements[key]; exists {
				grouped[key] = true
				databaseLines = append(databaseLines, key+"="+value)
			}
		}
	}
	// Recognize active assignments and Laravel's commented database examples.
	assignmentKey := func(line string) (string, bool) {
		trimmed := strings.TrimSpace(line)
		commented := strings.HasPrefix(trimmed, "#")
		if commented {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		}
		trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "export "), "export\t")
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			return "", commented
		}
		return strings.TrimSpace(key), commented
	}
	anchor := -1
	for i, line := range lines {
		key, _ := assignmentKey(line)
		if key == "DB_CONNECTION" && grouped[key] {
			anchor = i
			break
		}
	}
	if anchor == -1 {
		for i, line := range lines {
			key, _ := assignmentKey(line)
			if grouped[key] {
				anchor = i
				break
			}
		}
	}
	replaced := make(map[string]bool, len(replacements))
	result := make([]string, 0, len(lines)+len(replacements))
	for i, line := range lines {
		if i == anchor {
			result = append(result, databaseLines...)
		}
		key, commented := assignmentKey(line)
		if grouped[key] {
			replaced[key] = true
			continue
		}
		if value, ok := replacements[key]; ok && !commented {
			result = append(result, key+"="+value)
			replaced[key] = true
		} else {
			result = append(result, line)
		}
	}
	var missing []string
	if len(databaseLines) > 0 && anchor == -1 {
		missing = append(missing, databaseLines...)
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		if !replaced[key] && !grouped[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		missing = append(missing, key+"="+replacements[key])
	}
	if len(missing) > 0 {
		// Insert before the trailing newline rather than introducing a blank gap.
		if len(result) > 0 && result[len(result)-1] == "" {
			result = append(result[:len(result)-1], missing...)
			result = append(result, "")
		} else {
			result = append(result, missing...)
		}
	}
	return strings.Join(result, newline)
}

// Keep quoted multiline values together: assignment-like text inside a secret
// is data, and replacing a multiline assignment must remove its whole value.
func dotEnvStatements(content, newline string) []string {
	var statements []string
	var quote byte
	for _, line := range strings.Split(content, newline) {
		if quote != 0 {
			statements[len(statements)-1] += newline + line
			quote = openDotEnvQuote(line, quote)
			continue
		}
		statements = append(statements, line)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		_, value, ok := strings.Cut(trimmed, "=")
		value = strings.TrimSpace(value)
		if ok && len(value) > 0 && (value[0] == '\'' || value[0] == '"' || value[0] == '`') {
			quote = openDotEnvQuote(value[1:], value[0])
		}
	}
	return statements
}

// Inspect the last active assignment without treating multiline secret contents
// as assignments. Do not rewrite a supplied non-empty application name.
func hasDotEnvValue(content, name string) bool {
	value := ""
	for _, statement := range dotEnvStatements(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(statement)
		if strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "export "), "export\t"))
		key, candidate, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == name {
			value = strings.TrimSpace(candidate)
		}
	}
	if value == "" {
		return false
	}
	if value[0] == '\'' || value[0] == '"' || value[0] == '`' {
		quote := value[0]
		for i := 1; i < len(value); i++ {
			if value[i] == '\\' && i+1 < len(value) {
				i++
				continue
			}
			if value[i] == quote {
				return strings.TrimSpace(value[1:i]) != ""
			}
		}
		return true // Preserve malformed quoted input rather than silently replacing it.
	}
	value, _, _ = strings.Cut(value, "#")
	return strings.TrimSpace(value) != ""
}

func openDotEnvQuote(value string, quote byte) byte {
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			i++
		} else if value[i] == quote {
			return 0
		}
	}
	return quote
}
