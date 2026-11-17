package maintenancetask

// Closed raw-input contract for the reviewed Ubuntu Noble fixture. These are
// exact content approvals, not an assertion that every conffile variant from
// these packages is interchangeable. Optional package snippets may be absent;
// unknown or modified files require review and are never automatically repaired.
//
// Source ordering was checked in Ubuntu APT 2.8.3 and rechecked for
// 2.8.4ubuntu0.1 (exact archive hash and boundaries in apt_platform.go):
// https://archive.ubuntu.com/ubuntu/pool/main/a/apt/apt_2.8.3.tar.xz
//
//	apt-pkg/init.cc: pkgInitConfig loads APT_CONFIG, parts, then main config.
//	apt-private/private-cmndline.cc: binary-specific promotion before dispatch.
//	cmdline/apt-config.cc: architecture discovery can invoke dpkg before dump.
//	apt-private/private-install.cc: InstallPackages empty-plan return occurs
//	  before dpkg execution, but install/JSON hooks can execute earlier.
//	apt-pkg/deb/dpkgpm.cc: Go runs DPkg::Pre-Invoke before Pre-Install-Pkgs,
//	  package actions, logging and post-invoke hooks.
//	apt-private/private-update.cc: update hooks belong to DoUpdate, not the
//	  autoremove/autoclean handlers.
//
// The exact snippets below contain no global install/JSON hook, include,
// conditional Version subtree, Binary::apt-config/get masking, alternate root,
// executable override or nonstandard deletion path. Their permitted hooks are
// justified individually. This is not a proof against arbitrary privileged
// mutation or a qualification of unreviewed APT versions/platforms.
var nobleAPTConfigInputs = map[string]aptConfigInput{
	// APT vendor metadata: changelog/user-agent values; no execution directive.
	"01-vendor-ubuntu": {"364d5eeac5475b7dddfd629899ea88b91ed8d8e8e319c29bb9dbd6772e87ed55", true},
	// APT default kernel/firmware protection and automatic-package marking policy. Required to preserve the reviewed default intent.
	"01autoremove": {"35b4360d9126e5e5efc1ce42cea8a957ba42921de749ad8502922d536c9f385c", true},
	// Optional generated PostgreSQL 16 cluster protection: only NeverAutoRemove regexes, no executable directive.
	"02autoremove-postgresql": {"66ba028b70c0ae78940bc1bc3cbec070316550b00180034113aa3d4057e93227", false},
	// update-notifier periodic driver data; direct autoremove/autoclean does not run the periodic driver.
	"10periodic": {"82d9ce0d3f5d2c66945d8a1267445e9955409d1ef3cb9934107adccb46bf5f6a", false},
	// APT update-only post hook; neither reviewed cleanup handler dispatches DoUpdate.
	"15update-stamp": {"174cdb519fd06372847e23c20db869b2ff4b593252c0d2a6274d770eae2d92c9", false},
	// APT update pre-hook and Binary::apt upgrade JSON hook. Update is not dispatched; apt-config/apt-get do not promote Binary::apt.
	"20apt-esm-hook.conf": {"f7ab45bab1355711fcd031d78299cc3c7767654e7bf3f66a716ee3f2eae93733", false},
	// Periodic archive age/size policy data, with no executable or filesystem override.
	"20archive": {"4a569793b6a530904ff0dfd160bbc069c8a5b247d72f5756d62e6aeb31eabfdc", false},
	// Periodic update/unattended driver data; no driver is dispatched by direct cleanup.
	"20auto-upgrades": {"b81a3a01864d5ed7f8d023f3fa86d65087ad6b85706d399dc229a60b1c784807", false},
	// APT update hook and dpkg post hook. Update is not dispatched; the first failing dpkg Pre-Invoke prevents reaching post hooks.
	"20packagekit": {"549185db9256e57eaa25406d25ff52c339155cba1e5318148f1893bf728463c3", false},
	// Binary::apt install JSON hook only; neither apt-config nor apt-get promotes it.
	"20snapd.conf": {"4f1a710936d83d53caa575af269db4311d7b461bdb1bb94ced5b1d331b0886ac", false},
	// Index-target metadata plus an update-only post hook; no update operation is dispatched.
	"50appstream": {"8e89df0e4cfa24c7db4a165e776f40af7e0477f0d290bea4334edf6655db72bd", false},
	// Index-target metadata plus an update-only post hook; no update operation is dispatched.
	"50command-not-found": {"3a6f9f5244e49bee66ce65101fb73e16ba90f7a6cbd19befeefdf2c63209e2f1", false},
	// Unattended-driver origin/blacklist/default policy data. Direct cleanup never launches that Python driver.
	"50unattended-upgrades": {"ac8eb4fbfeb483815e5a7a224a86d4242f70bdb41ceb84333b05a9a39f5296be", false},
	// dpkg Pre-Install-Pkgs hook; Noble dpkgpm.cc runs the first refusing Pre-Invoke before this hook.
	"70debconf": {"db749e19baf3b72ca2c157c70c52522cae23d94bc8b2dc5793fd43d427445367", false},
	// dpkg post hook, unreachable after the first refusing Pre-Invoke.
	"99needrestart": {"d453d945957bf2fb33a26cb74eacd38244291919715a1bb1d6179d0290c95c52", false},
	// dpkg post and APT update post hooks; both unreachable through the qualified empty/guarded cleanup path.
	"99update-notifier": {"c01bac21ce3df26d6a5ef0bcd6fae53c924d6915121f20f1d412824367bfe930", false},
}
