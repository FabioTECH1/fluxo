package executiondomain

// Persistent249CandidateV1 is experimental and is not selected by any production
// caller. Its journal, one-use authorization and downgrade protocols have not
// been integrated or qualified. Native lifetime evidence alone cannot enable it.
const Persistent249CandidateV1 = "systemd-persistent-249-candidate-v1"

// Persistent249CandidateArguments describes the disabled v249 candidate. Do not
// add --collect: inactive-or-failed collection would change its retention
// contract. A failed/inactive unit is never evidence that its domain is empty.
func Persistent249CandidateArguments() []string {
	return []string{
		"--property=Type=exec", "--property=RemainAfterExit=yes", "--property=KillMode=none",
		"--property=Restart=no", "--property=CollectMode=inactive",
		"--property=User=root", "--property=Group=root", "--property=NoNewPrivileges=yes",
		"--property=CapabilityBoundingSet=CAP_SETUID CAP_SETGID CAP_DAC_READ_SEARCH",
		"--property=Delegate=no", "--property=ProtectControlGroups=yes", "--property=WorkingDirectory=/",
		"--property=RuntimeMaxSec=infinity", "--property=WatchdogSec=0",
		"--property=TimeoutStopSec=infinity", "--property=SendSIGKILL=no",
	}
}

// Persistent249CandidateProperties defines observed service properties, not a
// production authorization check. Callers must additionally bind boot, unit
// invocation ID, cgroup identity, executable, complete argv and durable intent.
func Persistent249CandidateProperties() map[string]string {
	return map[string]string{
		"Type": "exec", "RemainAfterExit": "yes", "KillMode": "none",
		"Restart": "no", "CollectMode": "inactive", "User": "root", "Group": "root",
		"NoNewPrivileges": "yes", "Delegate": "no", "ProtectControlGroups": "yes",
		"WorkingDirectory": "/", "RuntimeMaxUSec": "infinity", "WatchdogUSec": "0",
		"TimeoutStopUSec": "infinity", "SendSIGKILL": "no", "Transient": "yes",
		"ExecStartPre": "", "ExecStartPost": "", "ExecStop": "", "ExecStopPost": "", "DropInPaths": "",
	}
}
