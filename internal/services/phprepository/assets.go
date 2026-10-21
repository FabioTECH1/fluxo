// Package phprepository contains the release-pinned official Ubuntu repository
// key and fixed migration helper; no package maintainer scripts are executed.
package phprepository

import _ "embed"

//go:embed sury-keyring.gpg
var Key []byte

//go:embed repository.py
var Script string

const Fingerprint = "15058500A0235D97F5D10063B188E2B695BD4743"
