# Noble Cronie loader profile

`ubuntu-noble-cronie-loader-policy-v1` adds current loader qualification to the
unchanged historical `cronie-noble-1` identity. It is not a backend reader role,
a replacement identity, or permission to switch scheduling engines. The basic
historical verifier runs before and after the full closure, with exact identity
equality. Saved Cronie obligations select that verifier explicitly.

The native fixture is an isolated Ubuntu 24.04 amd64 installation with the actual
`cronie` package `1.7.1-1`, `/usr/sbin/crond`, ordinary `cronie.service`, and
published Fluxo `0.4.40`. It uses no production adapter/path overrides. Other
platforms, versions and customized stacks remain unqualified by this profile.

## Exact source inputs

The Cronie artifacts and SHA256 pins are retained in
[legacy migration evidence](legacy_migration.md#cronie-adapter-and-companion-evidence).
The fully patched tree was extracted with `dpkg-source` after verifying those
three previously reviewed archive digests. New source evidence is retained under
the private acceptance directory `cronie-compatibility/upstream/`.

The additional selected PAM module comes from Ubuntu `pam 1.5.3-5ubuntu5.7`:

- `pam_1.5.3.orig.tar.xz`: SHA256
  `7ac4b50feee004a9fa88f1dfd2d2fa738a82896763050cd773b3c54b0a818283`.
- `pam_1.5.3-5ubuntu5.7.debian.tar.xz`: SHA256
  `20a011e675ba5c2fdb22b681c395b3282128dfff119b8bec9adc39f4e87cddf9`.

These match the HTTPS-fetched `.dsc` manifest, an archive-consistency check rather
than a separate fresh source-signature claim. The VM's normal authenticated APT
installation supplies the actual package. Protected local dpkg metadata binds
the inspected executable and shared-library bytes, as in the existing finite
Noble vendor boundary; this is not a whole-OS integrity proof.

## Selected source paths

Cronie's patched `src/security.c` starts PAM service `cronie`, sets `PAM_TTY` to
`cron`, then calls account management, credential establishment, session opening,
group initialization and credential reinitialization. Permanent user switching
precedes the application exec. Closing removes credentials and closes/ends PAM.
Root jobs in system crontabs skip PAM; therefore a successful root receipt alone
does not establish the non-root PAM path. Native Scheduler/application execution
is a separate acceptance requirement.

The exact packaged stack adds `pam_access.so` to the existing reviewed Noble
modules. The module's `pam_sm_acct_mgmt` reaches `pam_sm_authenticate`, which
obtains the PAM user, resolves that user's account, reads the fixed tty/service,
then checks the default access policy and fragment glob. The profile pins the
exact inert vendor `access.conf` (SHA256
`f68915c4eb637aacbfa01cf26dc469a73f70acb0495efc4b074ecbc626bcb345`)
and accepts only absent/empty `/etc/security/access.d`. No active access rule
reaches its network/netgroup matching paths. Exact bytes avoid treating long C
parser records as comments. The pinned module stack has no accessfile/field/
separator overrides. This PAM build does not configure `VENDOR_SCONFIGDIR`;
the additional vendor fallback is not compiled in. Existing fallback-stack,
NSS/userdb, account, credential-metadata, ELF resolution and revalidation checks
remain mandatory.

`src/env.c:env_set_from_environ` copies the fixed locale-variable list and
`RANDOM_DELAY`. `src/security.c:build_env` merges PAM environment with crontab
environment, with the latter winning. PAM configuration/environment/locale files
stay exact pins. The fixed daemon command does not enable `-P`; dangerous
loader/NSS overrides are refused, and stable daemon environment participates in
the policy digest. Generation fields remain in the separate runtime digest.

The existing finite Noble SELinux boundary also applies: absent configuration,
policy mount and translation sockets, reviewed root and manager mount namespace.
Cronie's selected context paths in `src/security.c` and `src/user.c` resolve
labels and set the process exec context. As with Noble cron, current absence is
not proof of a library constructor's historical cached flags; both conditional
branches require source review. These context operations do not themselves grant
a PID1 attachment or independent process-domain migration. Active/custom SELinux
authority is outside this profile.

The prior actual loader/parser race and detached-child evidence remains relevant
to the exact same Cronie bytes. New current-loader and installed-upgrade results
must be recorded separately. `KillMode=process` is not a drainage proof: retained
domain identity and hierarchical emptiness remain required, including after the
daemon stops. No previously passing cron-only release result approves a new
Cronie execution image or public artifact.
