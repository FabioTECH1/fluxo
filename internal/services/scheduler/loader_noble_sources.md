# Noble loader profile source boundary

The separate loader profile retains the original empty Noble backend identity.
Its native data comes from the retained `fluxo-bridge-prod-20261101` VM; its V2
format floor was not changed by inspection. These are the exact patched sources
under `/tmp/fluxo-noble-loader-sources`:

| Component | Source version |
| --- | --- |
| cron | 3.0pl1-184ubuntu2 |
| PAM | 1.5.3-5ubuntu5.7 |
| libcap2/pam_cap | 1:2.66-5ubuntu2.4 |
| systemd/NSS | 255.4-1ubuntu8.18 |
| glibc | 2.39-0ubuntu8.9 |
| libselinux | 3.5-2ubuntu2.1 |

The archive SHA256/size checks match their exact `.dsc` manifests. Local source
manifest: `/tmp/fluxo-noble-loader-source-manifest.json`. Package/configuration
metadata: `/tmp/fluxo-noble-loader-public-metadata.json`. Source acquisition used
isolated lists/source/cache paths but initially inherited host APT update hooks.
Its metadata/service-hook footprint was recorded without reverting evidence in
`/tmp/fluxo-noble-loader-source-footprint.json`; the package comparison showed no
changes. Acquisition must not be described as entirely no-write. Future fetches
must also isolate APT main/config parts and clear hook namespaces.

PAM's Debian build has no vendor directory override, no libeconf build
dependency, and `--disable-nis`. The inspector additionally rejects actual ELF
libeconf dependencies/imports instead of relying on source defaults. Default
`pam_env` user environment reading is disabled. The active cron/common stacks
remain exact hash pins; fallback parsing is unchanged. libcap2 2.66 uses
`CAP_FILE_BUFFER_SIZE` and `fgets`, with delimiters space/TAB/newline. The existing
4096-byte/ASCII capability-file restrictions therefore apply to this profile.

The native NSS configuration selects files+systemd for passwd, group, shadow
and gshadow, with no separate initgroups override. The exact systemd255 sources
are `src/nss-systemd/nss-systemd.c`, `src/shared/userdb.c` and
`src/core/core-varlink.c`. The DynamicUser identity handlers return user and
group records; `GetMemberships` explicitly returns `NoRecordFound`. Their internal dynamic-user lookup/cache access
was included in the bounded review: lookup pops/restores an existing storage
record and does not acquire a new dynamic identity. Method names alone were
not treated as proof. The native userdbd service/socket and worker
are absent; only the root-protected DynamicUser listener is accepted, and every
matching kernel listener must be held by authenticated PID1.

SELinux conditional paths must be reviewed even with the native no-selinuxfs,
no-config and no-setrans-socket observations. `libselinux/src/init.c` caches
`selinux_mnt` and config presence in its constructor. `enabled.c` consults these
cached values. Present filesystem absence does not establish their historical
memory contents.

The selected cron call tree is:

- `user.c:load_user` conditionally calls `get_security_context`. It obtains the
  current context, local/NSS user mapping, ordered contexts, crontab context and
  a kernel entrypoint access decision. The implementation is in `seusers.c`,
  `get_context_list.c`, `getfilecon.c`, `compute_av.c`, `getenforce.c` and
  `stringrep.c`. These paths parse protected policy files, perform the reviewed
  NSS lookups and query proc/SELinux kernel interfaces.
- `do_command.c:child_process` conditionally calls `setexeccon` before its fixed
  job execution. `procattr.c` translates the context and writes the calling
  thread's `exec` security attribute. It does not itself execute a helper or
  move a cgroup/namespace. Kernel security checks can deny execution.
- `setrans_client.c` can translate through the fixed setrans socket. It opens a
  new socket per request; the constructor-like cache and previous translation
  strings are not proof of a currently available endpoint. Both canonical
  socket paths must be absent. There is no selected external command launch or
  PID1 process-attachment request in this translation path.

The independent bounded source review found no blocking hierarchy/authority
defect in either selected SELinux branch. Cached flags select the reviewed
context handling and do not add a launch or PID1 attachment request.
These selected paths do not justify accepting arbitrary SELinux configuration,
an active policy mount, alternate mount namespaces, custom NSS providers or a
translation service. The profile rejects those observable states. The native
inspection and source results are closure/configuration acceptance. The final
schema5 bootstrap/worker/application/generation matrix remains a separate
production gate.


## November 2026 Noble libc security update

The current exact libc6 qualification pin is `2.39-0ubuntu8.12`; historical
epoch digests and archives retain their original interpretation. Updated hosts
require explicit old/new policy review and publication, not automatic policy
equality bypass. Deleted old-library mappings continue to refuse qualification.

The official Ubuntu packaging source archive
[`glibc_2.39-0ubuntu8.12.debian.tar.xz`](https://archive.ubuntu.com/ubuntu/pool/main/g/glibc/glibc_2.39-0ubuntu8.12.debian.tar.xz)
matched its HTTPS-fetched `.dsc` SHA256
`cc7cf38a3060a282011e83c946b6cec1aebe9b04e21e133449abe28340a710f6`.
This is an archive/manifest consistency check, not an independent signature
provenance claim. The changelog proceeds directly from `.12` to `.9`.
[Ubuntu USN-9065-1](https://ubuntu.com/security/notices/USN-9065-1) identifies
this Noble security update.

Independent bounded source review covered the production patch hunks:
AT_SECURE ORIGIN normalization in `elf/dl-load.c` and `dl-path-normalize.h`;
empty ccs rejection in `libio/fileops.c`; LANGUAGE traversal rejection in
`intl/dcigettext.c`; DNS search-list consistency in `resolv/resolv_conf.c`;
and checked heap allocation in nscd caches. Power8 bounds and s390x alignment
changes do not affect the accepted amd64 ELF architecture. Loader prerequisite
patches modify test/support code. No new cgroup/PID1 attachment or module
selector was found in these changes. The profile still excludes nscd and
requires protected traversal, authenticated vendor payloads, an unambiguous
ELF dependency closure and native acceptance. This does not claim a complete
glibc audit or runtime acceptance.
