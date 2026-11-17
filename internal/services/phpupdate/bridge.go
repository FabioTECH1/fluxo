package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// RepositoryBridgeReady verifies policy using the existing bounded repository
// status operation. It never applies a plan or updates an APT source.
func RepositoryBridgeReady(ctx context.Context, dataDir string) error {
	raw, err := repositoryBridge(ctx, "", map[string]any{"action": "repository_status"})
	if err != nil {
		return err
	}
	var c repositoryConfiguration
	if err = json.Unmarshal(raw, &c); err != nil {
		return err
	}
	var auth struct {
		PolicyHash string `json:"policy_hash"`
		KeySHA256  string `json:"key_sha256"`
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "php-repository", "authorization.json"))
	if err != nil {
		return err
	}
	if json.Unmarshal(b, &auth) != nil || !repositoryConfigurationCurrent(c, auth.PolicyHash, auth.KeySHA256) {
		return errors.New("complete reviewed PHP repository setup before certification")
	}
	return nil
}
