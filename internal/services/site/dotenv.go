package site

import "strings"

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
	lines := strings.Split(content, "\n")
	replaced := make(map[string]bool, len(replacements))
	for i, line := range lines {
		for key, value := range replacements {
			if strings.HasPrefix(line, key+"=") {
				lines[i] = key + "=" + value
				replaced[key] = true
			}
		}
	}
	for key, value := range replacements {
		if !replaced[key] {
			lines = append(lines, key+"="+value)
		}
	}
	return strings.Join(lines, "\n")
}
