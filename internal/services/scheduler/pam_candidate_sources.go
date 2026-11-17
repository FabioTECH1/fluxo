package scheduler

// Source review boundary for disabled-cron-pam-nss-v1:
//
// Ubuntu22: pam 1.4.0-11ubuntu2.8, libcap2 1:2.44-1ubuntu0.22.04.3,
// systemd 249.11-0ubuntu3.23. Ubuntu26: pam 1.7.0-5ubuntu3.2,
// libcap2 1:2.75-10ubuntu2, systemd 259.5-0ubuntu3.4. Fully patched source
// trees supplied under /tmp/fluxo-{jammy,resolute}-{pam,nss}-sources are the
// evidence used here, not a claim that upstream HEAD matches installed Ubuntu.
//
// cron do_command.c calls pam_start("cron"), pam_acct_mgmt, pam_setcred
// ESTABLISH, pam_open_session, then setgid/initgroups and setuid;26 additionally
// reinitializes credentials after setuid. Cleanup calls DELETE/close/end. It
// does not authenticate passwords or call chauthtok. All function-specific
// stacks are nevertheless parsed/loaded by libpam/pam_handlers.c, including
// "other" -> common-password/common-session. Existing hashes pin the cron and
// active common-* policy; the additional fallback parser permits only known
// modules/arguments and includes. No pam_exec, pam_systemd active session,
// pam_namespace, pam_mount, user-owned module, or include path is admitted.
//
// Reachable hook review:
// - pam_unix_acct.c: fixed unix_chkpwd "chkexpiry" child uses an empty
//   environment and a bounded pipe/waitpid. unix_chkpwd reads local credentials,
//   reports status and exits; no application-job or cgroup authority is handed
//   to it. Its actual ELF/dependencies are authenticated even when root avoids
//   this fallback. pam_unix_auth.c setcred returns its saved result; the cron
//   path never calls authenticate or password update helpers. pam_unix_sess.c
//   open/close read the username/service and log; they do not launch programs.
// - pam_cap/pam_cap.c setcred can alter inheritable/ambient capabilities. The
//   active stock common-auth file has no keepcaps/defer/config/default arguments;
//   capability.conf must be absent, comments/empty, or one exact none-wildcard
//   rule (clears inheritable/ambient caps in both versions). Thus no rule
//   grants new capabilities. Do not weaken this to arbitrary protected content.
// - pam_loginuid: writes process audit login identity/session through procfs and
//   audit APIs. It does not relocate execution groups or create a service.
// - pam_env: default user_readenv is disabled; only the stock pinned environment,
//   pam_env.conf and explicit locale file are reachable. No user environment
//   file, alternate conffile, or execution-affecting module argument is enabled.
// - pam_umask: opens /etc/login.defs and /etc/default/login; applies umask,
//   USERGROUPS_ENAB, and optional GECOS umask/priority/file-limit values. The two
//   protected files are rebound and root/fluxo GECOS override directives rejected.
// - pam_limits: pinned limits.conf and exact limits.d membership select resource
//   limits/priority/account-session checks. Root/nonroot execution is still
//   checked by the separate real cron minute tests. Unknown fragments refuse.
// - pam_deny/pam_permit return fixed status. pam_systemd may be loaded from
//   'other', but cron has explicit open/close-session stacks, so the relocation
//   hook is not dispatched. The fallback code and ELF dependencies still require
//   authentic reviewed vendor payloads. Missing active stacks fail the hash pin.
//
// PAM26 Debian meson rules do not override the empty vendordir default, and
// Build-Depends has no libeconf-dev. Actual ELF NEEDED/imported symbols are
// checked for econf before excluding that parser branch; an enabled branch
// refuses. libpam always-present /usr/lib/pam.d fallback cannot replace the
// required protected /etc/pam.d files while the input observations remain valid.
//
// NSS: files+systemd passwd/group/initgroups. Ubuntu22 shadow/gshadow use files;
// Ubuntu26 also uses the exact systemd shadow/group-shadow lookup chain.
// nss-systemd.c lookup/getgr*/initgroups, userdb-glue.c and shared/userdb.c send
// GetUserRecord/GetGroupRecord/GetMemberships requests and copy numeric records;
// they do not issue execution or group-attachment requests. Custom provider
// modules, nscd, userdb drop-in records and provider endpoint names refuse. Stock
// root-protected systemd provider endpoints and authentic vendor libc/systemd
// lookup implementations are an explicit finite OS trust boundary. This is not
// a whole-systemd service audit or a proof against privileged vendor replacement.
//
// The PAM Unix DSO also links libnsl/tirpc and standard Kerberos dependencies.
// Their authentic vendor constructors are accepted inside the finite OS trust
// boundary. Selected cron account/setcred/session hooks do not call NIS or
// password-changing RPC; their unused service/plugin configurations are not
// treated as reachable without evidence of a constructor or selected-hook path.
//
// Loader closure: no ldd execution. Protected vendor dpkg MD5 metadata binds
// reachable code (not repository signature provenance). Actual ELF dependencies,
// interpreter, RPATH/RUNPATH, loader cache/config, absent preloads/hwcaps and live
// cron executable mappings are inspected. All possible observed resolutions for
// a needed SONAME must agree on one authenticated vendor payload. LD_*, tunable,
// NSS/userdb and locale/gconv loader overrides refuse. Input metadata is checked
// again after inspection; identity credentials are never hashed into evidence.
//
// Remaining integration gates: native closure on both actual hosts; independent
// review of this finite trust boundary and parser/refusal coverage; composition
// with real root/nonroot PAM containment/identity/progress/natural-drain evidence;
// an explicit versioned new backend contract plus activation/rollback support.
// This code never enables a production selector or writes backend proof journals.
