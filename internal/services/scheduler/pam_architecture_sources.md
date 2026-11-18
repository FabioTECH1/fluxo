# ARM64 loader platform definition (candidate)

This port adds architecture data to the existing finite Ubuntu vendor trust
boundary. It does not certify all code in libc/systemd, change historical backend
identity meanings, or replace genuine current loader acknowledgement. Main and
published ARM support remain unchanged until native acceptance completes.

The amd64 triplet, interpreter, cache interpretation, revision and configuration
pins are preserved. ARM64 selects `aarch64-linux-gnu`, ELF64 little endian,
`EM_AARCH64`, and `/lib/ld-linux-aarch64.so.1`. The architecture comes from the
executing Go image, never an environment override or a saved host label.

Primary source references inspected on 2026-11-18:

- glibc [2.35 AArch64 cache selection](https://github.com/bminor/glibc/blob/release/2.35/master/sysdeps/unix/sysv/linux/aarch64/dl-cache.h)
  and [2.39 counterpart](https://github.com/bminor/glibc/blob/release/2.39/master/sysdeps/unix/sysv/linux/aarch64/dl-cache.h)
  require exact LP64 cache flags `FLAG_AARCH64_LIB64 | FLAG_ELF_LIBC6` (`0x0a03`).
  The ILP32 ABI is distinct and is not accepted.
- glibc [cache constants](https://github.com/bminor/glibc/blob/release/2.39/master/sysdeps/generic/ldconfig.h),
  [2.35 capability search](https://github.com/bminor/glibc/blob/release/2.35/master/elf/dl-hwcaps.c),
  and [AArch64 capability selection](https://github.com/bminor/glibc/blob/release/2.35/master/sysdeps/unix/sysv/linux/aarch64/dl-procinfo.h).
  Jammy retains legacy capability search; any populated `tls`, `atomics`, or
  `aarch64` root is refused along every inspected search/RPATH directory, covering
  nested combinations without assuming the current CPU lacks a feature.
- Linux [arm64 ELF platform](https://github.com/torvalds/linux/blob/v6.8/arch/arm64/include/asm/elf.h)
  defines `aarch64` for the admitted little-endian ABI.
- glibc [2.39 capability search](https://github.com/bminor/glibc/blob/release/2.39/master/elf/dl-hwcaps.c)
  removes the legacy powerset search. Existing populated `glibc-hwcaps` refusal
  remains in every profile.

Native inventory [44318752237](https://github.com/FabioTECH1/fluxo/actions/runs/44318752237)
ran on all three native GitHub Ubuntu ARM runners. Their caches contained only
`0x0a03` entries and their loader declarations used the expected multiarch paths.
Authenticated APT metadata/downloads reported these cron archive hashes:

| Ubuntu | cron version | ARM64 `.deb` SHA256 |
| --- | --- | --- |
| 22.04 | 3.0pl1-137ubuntu3 | `7e6575e4a6b8abcb3f1c12e25d4650e313ecfdb530f2ceb861b3c01d30e64923` |
| 24.04 | 3.0pl1-184ubuntu2 | `f29126ccb78db3e21b54bb1e351a830bc90e1a0c8eff0a1eb77233384c9cbcf8` |
| 26.04 | 3.0pl1-200ubuntu1 | `f022fce2059f2f93f97a2b8e382916f6163f9a130dcbe7a43146ccdcf05690e0` |

Installed binary fingerprints from that inventory are separately pinned in the
candidate definitions. Inventory/hash equality is not runtime acceptance. Noble
and Cronie retain their existing historical ARM fingerprints and configuration
digests; current closure uses a separate ARM revision.

The CI runners are not pristine VPS images: they append a PAM limits rule and
preinstall unrelated tools/repos. Test fixture preparation is recorded separately
and does not add production hostnames, environment bypasses, or customer steps.
The newer 26.04 systemd package is not silently added to the existing profile.
