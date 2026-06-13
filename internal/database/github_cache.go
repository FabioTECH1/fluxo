package database

// GetCachedGitHubData returns cached JSON data for the given key.
func GetCachedGitHubData(key string) (string, bool) {
	var data string
	err := DB.QueryRow("SELECT data FROM github_cache WHERE key = ?", key).Scan(&data)
	if err != nil {
		return "", false
	}
	return data, true
}

// SetCachedGitHubData stores or updates JSON data for the given key.
func SetCachedGitHubData(key, data string) {
	DB.Exec("INSERT INTO github_cache (key, data, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP) ON CONFLICT(key) DO UPDATE SET data = ?, updated_at = CURRENT_TIMESTAMP", key, data, data)
}
