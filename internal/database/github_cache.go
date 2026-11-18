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

// SetCachedGitHubDataForAccount prevents in-flight requests using a replaced or
// disconnected credential from repopulating the account's metadata cache. The
// identity check and cache write are one SQLite statement, so token rotation's
// cache invalidation cannot race an old-token background refresh.
func SetCachedGitHubDataForAccount(key, data string, accountID int, storedToken string) error {
	_, err := DB.Exec(`INSERT INTO github_cache(key,data,updated_at)
 SELECT ?,?,CURRENT_TIMESTAMP WHERE EXISTS(SELECT 1 FROM github_accounts WHERE id=? AND token=?)
 ON CONFLICT(key) DO UPDATE SET data=excluded.data,updated_at=CURRENT_TIMESTAMP`, key, data, accountID, storedToken)
	return err
}
