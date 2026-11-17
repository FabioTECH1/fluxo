package maintenancetask

// Closed raw input approvals for Ubuntu jammy / APT 2.4.14 and 2.4.15ubuntu0.1.
// Security-update source evidence is recorded in apt_platform.go. The shared
// checker still rejects every unknown entry and main-config override. Common
// snippets have the same individually reviewed meanings as the Noble table.
//
// Reviewed differences:
//
//	01autoremove retains additional archive-section protection/marking regexes.
//	20packagekit has older comments; its only actions remain update/post-dpkg.
//	99needrestart omits -m u, but is still post-dpkg and behind the refusal.
//
// No snippet adds an include, global install/JSON hook, executable/path
// override, conditional Version scope or apt-config/get binary masking.
// Runtime qualification remains separate: source review identified method
// capability startup before the dpkg guard even with --no-download.
var jammyAPTConfigInputs = map[string]aptConfigInput{
	"01-vendor-ubuntu": {"364d5eeac5475b7dddfd629899ea88b91ed8d8e8e319c29bb9dbd6772e87ed55", true},
	"01autoremove":     {"93e7e6d2fdb36b04cb10127e3b0d1b9d19d822327fd959484639bbbd65cce004", true},
	// Stock PostgreSQL 14 cluster protection; only a NeverAutoRemove regex.
	"02autoremove-postgresql": {"c45bd49a928b0f4be8c056c6674a581c37361a0d3d0a01c1b3885f26d2bde0ac", false},
	"10periodic":              {"82d9ce0d3f5d2c66945d8a1267445e9955409d1ef3cb9934107adccb46bf5f6a", false},
	"15update-stamp":          {"174cdb519fd06372847e23c20db869b2ff4b593252c0d2a6274d770eae2d92c9", false},
	"20apt-esm-hook.conf":     {"f7ab45bab1355711fcd031d78299cc3c7767654e7bf3f66a716ee3f2eae93733", false},
	"20archive":               {"4a569793b6a530904ff0dfd160bbc069c8a5b247d72f5756d62e6aeb31eabfdc", false},
	"20auto-upgrades":         {"b81a3a01864d5ed7f8d023f3fa86d65087ad6b85706d399dc229a60b1c784807", false},
	"20packagekit":            {"4d41820beb3b91ae4096114761d8321069362777b8c3dc196a8d20bbad755864", false},
	"20snapd.conf":            {"4f1a710936d83d53caa575af269db4311d7b461bdb1bb94ced5b1d331b0886ac", false},
	"50command-not-found":     {"3a6f9f5244e49bee66ce65101fb73e16ba90f7a6cbd19befeefdf2c63209e2f1", false},
	"50unattended-upgrades":   {"ac8eb4fbfeb483815e5a7a224a86d4242f70bdb41ceb84333b05a9a39f5296be", false},
	"70debconf":               {"db749e19baf3b72ca2c157c70c52522cae23d94bc8b2dc5793fd43d427445367", false},
	"99needrestart":           {"d19cd067f6d3eaaa7cc47a0cac3aed503388e649f4e9a6b0da809588fe060e6e", false},
	"99update-notifier":       {"c01bac21ce3df26d6a5ef0bcd6fae53c924d6915121f20f1d412824367bfe930", false},
}
