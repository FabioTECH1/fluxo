package maintenancetask

// Closed raw input approvals for Ubuntu resolute / APT 3.2.0 and 3.2.1ubuntu0.1.
// Security-update source evidence is recorded in apt_platform.go. The shared
// checker still rejects every unknown entry and main-config override. Common
// snippets have the same individually reviewed meanings as the Noble table.
//
// Reviewed differences:
//
//	20packagekit adds an ostree guard to otherwise unchanged unreachable hooks.
//	50unattended-upgrades changes commentary and documents postponement;
//	  direct apt-get cleanup does not invoke the unattended driver.
//
// No snippet adds an include, global install/JSON hook, executable/path
// override, conditional Version scope or apt-config/get binary masking.
// This is a candidate, not authorization to run APT 3.2 cleanup: its solver-3
// comparison/fallback path is reachable even with Strict-Pinning's default.
var resoluteAPTConfigInputs = map[string]aptConfigInput{
	"01-vendor-ubuntu": {"364d5eeac5475b7dddfd629899ea88b91ed8d8e8e319c29bb9dbd6772e87ed55", true},
	"01autoremove":     {"35b4360d9126e5e5efc1ce42cea8a957ba42921de749ad8502922d536c9f385c", true},
	// Stock PostgreSQL 18 cluster protection; only a NeverAutoRemove regex.
	"02autoremove-postgresql": {"ef080421e56b6d3a6a847d7737aa6c0d7ea71e463e408a918c10af2d35e1968c", false},
	"10periodic":              {"82d9ce0d3f5d2c66945d8a1267445e9955409d1ef3cb9934107adccb46bf5f6a", false},
	"15update-stamp":          {"174cdb519fd06372847e23c20db869b2ff4b593252c0d2a6274d770eae2d92c9", false},
	"20apt-esm-hook.conf":     {"f7ab45bab1355711fcd031d78299cc3c7767654e7bf3f66a716ee3f2eae93733", false},
	"20archive":               {"4a569793b6a530904ff0dfd160bbc069c8a5b247d72f5756d62e6aeb31eabfdc", false},
	"20auto-upgrades":         {"b81a3a01864d5ed7f8d023f3fa86d65087ad6b85706d399dc229a60b1c784807", false},
	"20packagekit":            {"b4b98908ac8561a4823b23c724e65cc4c98f2067eb961a80816b3d15427522b4", false},
	"20snapd.conf":            {"4f1a710936d83d53caa575af269db4311d7b461bdb1bb94ced5b1d331b0886ac", false},
	"50appstream":             {"8e89df0e4cfa24c7db4a165e776f40af7e0477f0d290bea4334edf6655db72bd", false},
	"50command-not-found":     {"3a6f9f5244e49bee66ce65101fb73e16ba90f7a6cbd19befeefdf2c63209e2f1", false},
	"50unattended-upgrades":   {"88fb5cc2a1bbe749468213f6a9d351a8b668760d8a4c94eef5ad7f4723f19a6e", false},
	"70debconf":               {"db749e19baf3b72ca2c157c70c52522cae23d94bc8b2dc5793fd43d427445367", false},
	"99needrestart":           {"d453d945957bf2fb33a26cb74eacd38244291919715a1bb1d6179d0290c95c52", false},
	"99update-notifier":       {"c01bac21ce3df26d6a5ef0bcd6fae53c924d6915121f20f1d412824367bfe930", false},
}
