package maintenancetask

import "context"

type Diagnostic struct {
	Kind      Kind   `json:"kind"`
	Supported bool   `json:"supported"`
	Error     string `json:"error,omitempty"`
	Notice    string `json:"notice,omitempty"`
}

// Diagnostics qualifies inputs only. It does not initialize state, publish a
// cron trigger, launch a job, invoke PHP/Python application code or change APT.
func Diagnostics(ctx context.Context) []Diagnostic {
	var result []Diagnostic
	for _, kind := range []Kind{PackageCleanup, CertificateRenewal, ComposerUpdate} {
		p, err := qualifyProfile(ctx, kind)
		d := Diagnostic{Kind: kind, Supported: err == nil}
		if err != nil {
			d.Error = err.Error()
		} else if p.External[certbotManualProfileKey] == certbotManualNoHooksContract {
			d.Notice = manualCertbotNotice
		}
		result = append(result, d)
	}
	return result
}
