// Package sitepolicy defines current managed website settings without database or runtime dependencies.
package sitepolicy

type SiteRedirect struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Status      int    `json:"status"`
}
type SiteNetworkSettings struct {
	Format       int            `json:"format"`
	Enabled      bool           `json:"enabled"`
	Username     string         `json:"username"`
	PasswordHash string         `json:"password_hash"`
	Redirects    []SiteRedirect `json:"redirects"`
	Wildcards    []string       `json:"wildcards"`
}
