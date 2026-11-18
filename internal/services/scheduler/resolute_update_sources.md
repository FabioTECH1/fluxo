# Resolute ARM systemd 259.9 input profile

This profile is limited to ARM64 and exact systemd 259.9-0ubuntu0.1 packages.
The original 259.5 profile, AMD64 selection, backend labels, reader roles and
stored identities remain unchanged. A new versioned loader profile/revision is
included in the policy and configuration digests. Old configuration equality
still refuses drift; this does not authorize rewriting older saved proofs.

Official Ubuntu source archives were downloaded from
https://ports.ubuntu.com/ubuntu-ports/pool/main/s/systemd/ and checked against the
SHA256/size tables in their exact HTTPS `.dsc` manifests (not a claim of `.dsc`
signature verification):

| Archive | SHA256 |
| --- | --- |
| systemd_259.5.orig.tar.gz | 80ed55a8a69c4bd1fb12a36659303372b37baf9ee224ef4f032db4b748be0f76 |
| systemd_259.5-0ubuntu3.4.debian.tar.xz | 34048bc4b67dce61835a5a959d903f912d68bb09a93ba654e6ae33410c74aaa9 |
| systemd_259.9.orig.tar.gz | 6ac77a1baf0539b835193eec46cdb1c73abb42d7f87aa977009150e9d8375757 |
| systemd_259.9-0ubuntu0.1.debian.tar.xz | c58ab8296f580f8760afc945261917fa23dfb62df8a78121a5967c5b237a24cc |

Focused review compared NSS, userdb glue/records and PAM plus their Debian patches:
NSS recursion suppression changes ELF TLS into pthread-specific integer counters;
allocation errors can return normal errors or leave the query unblocked, so this
is not described as a universal fail-closed lookup guarantee. No execution or
cgroup-attachment request is added. Group shadow packing corrects the pointer
array offset. Group/userdb matching adds a UUID filter. User record loops use the
sized string-vector helper (also present in the older Ubuntu security patch).
PAM systemd source and its applicable Debian session/device-access patches are
unchanged. Cron's pinned noninteractive stack still does not dispatch its session
relocation hook. Existing protected account/NSS/provider, manager environment,
unit/worker absence and ELF/dependency checks remain mandatory.

The current vendor `20-coredump-debian.conf` sets only soft RLIMIT_CORE to infinity
for wildcard/root identities. Its exact bytes are pinned. Fresh installs may have
only this file; upgrades may additionally retain the exact old `10-` file. Those
are two explicit inventories; additional or modified fragments refuse. No job
launch, namespace, UID/GID or cgroup policy is selected by these limit rules.
This is not a whole-systemd audit or a coredump confidentiality certification.

Runtime code remains authenticated through protected native dpkg payload metadata,
inside the existing finite vendor/administrator trust boundary. Full native
qualification, genuine receipts, execution/drainage, negative checks and installed
upgrade acceptance are separate gates and remain pending until recorded.
