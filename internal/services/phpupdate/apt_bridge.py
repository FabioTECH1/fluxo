"""Fixed python-apt adapter, embedded in Fluxo; never accepts Python/shell code.

Planning and commit use the same libapt resolver. Apply opens its cache AFTER
obtaining APT's native system lock and compares the complete family/transaction
under that lock. Debian's package frontend lock remains held through commit.
"""
import contextlib
import hashlib
import json
import os
import re
import resource
import shutil
import stat
import sys
import time
import tempfile
import urllib.parse

import apt
import apt_pkg
import apt.progress.base
import apt_inst

apt_pkg.init()
apt_pkg.config.set("Acquire::Retries", "2")
apt_pkg.config.set("Acquire::http::Timeout", "30")
apt_pkg.config.set("Acquire::https::Timeout", "30")
apt_pkg.config.set("APT::Get::AllowUnauthenticated", "false")
apt_pkg.config.set("APT::Install-Recommends", "false")
apt_pkg.config.set("Dpkg::Use-Pty", "false")
apt_pkg.config.set("DPkg::Options::", "--force-confold")
PATTERN = re.compile(r"^php([0-9]+\.[0-9]+)(?:-|$)")
CONTROL_FIELDS = ("Package", "Version", "Architecture", "Source", "Depends", "Pre-Depends",
                  "Conflicts", "Breaks", "Replaces", "Provides", "Multi-Arch", "Essential")
TRUSTED_ORIGINS = {"Ubuntu", "UbuntuESM", "UbuntuESMApps", "LP-PPA-ondrej-php"}


def emit(value):
    print("FLUXO_JSON:" + json.dumps(value, sort_keys=True), flush=True)


def policy_hash():
    h = hashlib.sha256()
    for base in ["/etc/apt/sources.list", "/etc/apt/sources.list.d", "/etc/apt/preferences", "/etc/apt/preferences.d"]:
        files = [base]
        if os.path.isdir(base):
            files = [os.path.join(base, x) for x in sorted(os.listdir(base))]
        for path in files:
            if os.path.isfile(path):
                h.update(path.encode())
                with open(path, "rb") as f:
                    h.update(f.read())
    return h.hexdigest()


def origins(v):
    return sorted([dict(origin=o.origin, label=o.label, archive=o.archive,
                        component=o.component, site=o.site, trusted=o.trusted)
                   for o in v.origins if o.archive != "now"],
                  key=lambda x: json.dumps(x, sort_keys=True))


def archive_metadata(v):
    digest = v.record.get("SHA256", "")
    source_origins = origins(v)
    if not re.fullmatch(r"[a-fA-F0-9]{64}", digest) or v.size <= 0 or not source_origins:
        return None
    if any(not o["trusted"] or o["origin"] not in TRUSTED_ORIGINS for o in source_origins):
        return None
    return dict(name=v.record.get("Package", ""), architecture=v.architecture,
                version=v.version, sha256=digest.lower(), size=v.size,
                control={key: v.record.get(key, "") for key in CONTROL_FIELDS},
                origins=source_origins)


def package(p):
    i, c = p.installed, p.candidate
    value = dict(name=p.shortname, architecture=i.architecture, installed=i.version,
                candidate=c.version if c else "", source=i.source_name,
                source_version=i.source_version,
                candidate_source=c.source_name if c else "",
                candidate_source_version=c.source_version if c else "",
                sha256=c.record.get("SHA256", "") if c else "",
                size=c.size if c else 0, origins=origins(c) if c else [],
                held=p._pkg.selected_state == apt_pkg.SELSTATE_HOLD,
                configured=p._pkg.current_state == apt_pkg.CURSTATE_INSTALLED,
                automatic=p.is_auto_installed)
    metadata = archive_metadata(i)
    if metadata:
        value["installed_archive"] = metadata
    return value


def private_directory(path):
    if not os.path.isabs(path) or os.path.realpath(path) != path:
        raise RuntimeError("Archive directory must be an absolute, non-symlink path")
    current = path
    while current != "/":
        info = os.lstat(current)
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise RuntimeError("Archive directory is not protected by root ownership")
        current = os.path.dirname(current)


def verify_archive(path, metadata):
    info = os.lstat(path)
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or info.st_mode & 0o022:
        raise RuntimeError("Unsafe package archive file")
    if info.st_size != metadata["size"]:
        raise RuntimeError("Package archive size differs from authenticated metadata")
    h = hashlib.sha256()
    with open(path, "rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            h.update(block)
    if h.hexdigest() != metadata["sha256"]:
        raise RuntimeError("Package archive checksum verification failed")
    control = apt_pkg.TagSection(apt_inst.DebFile(path).control.extractdata("control").decode("utf-8"))
    if (control["Package"], control["Architecture"], control["Version"]) != (metadata["name"], metadata["architecture"], metadata["version"]):
        raise RuntimeError("Package archive identity differs from the approved package")
    if any(control.get(key, "") != value for key, value in metadata["control"].items()):
        raise RuntimeError("Package control fields differ from authenticated metadata")


def verify_native_payloads(directory, targets, bindings):
    for binding in bindings:
        if not re.fullmatch(r"php[0-9]+\.[0-9]+-swoole", binding["package"]) or not re.fullmatch(r"usr/lib/php/[0-9]{8}/swoole\.so", binding["member"]) or not re.fullmatch(r"[a-f0-9]{64}", binding["sha256"]) or not 0 < binding["size"] <= 64 * 1024**2:
            raise RuntimeError("Unsupported native recovery archive binding")
        matches = [p for p in targets if p["name"] == binding["package"] and p["architecture"] == binding["architecture"]]
        if len(matches) != 1:
            raise RuntimeError("Native runtime archive is missing from the retained set")
        path = os.path.join(directory, matches[0]["sha256"] + ".deb")
        # Only inspect the one library in memory; never extract an archive onto
        # the filesystem. Bound allocations even for a malformed archive member.
        old_limit = resource.getrlimit(resource.RLIMIT_AS)
        with open("/proc/self/statm", encoding="ascii") as statm:
            current = int(statm.read().split()[0]) * os.sysconf("SC_PAGE_SIZE")
        limit = current + 128 * 1024**2
        if old_limit[0] != resource.RLIM_INFINITY:
            limit = min(limit, old_limit[0])
        if old_limit[1] != resource.RLIM_INFINITY:
            limit = min(limit, old_limit[1])
        found = []
        def inspect_member(member, data):
            if not member.isfile() or member.size != binding["size"] or len(data) != binding["size"] or hashlib.sha256(data).hexdigest() != binding["sha256"]:
                raise RuntimeError("Retained Swoole library differs from the approved running native runtime; no managed native upgrade is permitted")
            found.append(member.name)
        try:
            resource.setrlimit(resource.RLIMIT_AS, (limit, old_limit[1]))
            archive = apt_inst.DebFile(path)
            try:
                archive.data.go(inspect_member, "./" + binding["member"])
            except LookupError:
                archive.data.go(inspect_member, binding["member"])
        finally:
            resource.setrlimit(resource.RLIMIT_AS, old_limit)
        if len(found) != 1:
            raise RuntimeError("Retained native runtime library is missing or ambiguous")


def atomic_json(path, value):
    temp = path + ".partial"
    with open(temp, "x", encoding="utf-8") as target:
        json.dump(value, target, sort_keys=True)
        target.flush()
        os.fsync(target.fileno())
    os.replace(temp, path)
    fd = os.open(os.path.dirname(path), os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def prepare_archives(cache, expected, directory, acknowledge, native_bindings=None):
    private_directory(directory)
    if os.path.exists(os.path.join(directory, "manifest.json")):
        raise RuntimeError("A package archive manifest already exists for this operation")
    before, after = [], []
    for item in expected["packages"]:
        pkg = cache[item["name"] + ":" + item["architecture"]]
        target = next((v for v in pkg.versions if v.version == item["candidate"]), None)
        metadata = archive_metadata(target) if target else None
        if not metadata or metadata["sha256"] != item["sha256"]:
            raise RuntimeError("Authenticated target archive is unavailable for " + item["name"])
        after.append(metadata)
        before.append(item.get("installed_archive"))
    wanted = {m["sha256"]: m for m in before + after if m}
    size = sum(m["size"] for m in wanted.values())
    if size > 2 * 1024**3 or shutil.disk_usage(directory).free < size * 3 + 256 * 1024**2 or os.statvfs(directory).f_favail < 1024:
        raise RuntimeError("Insufficient recovery storage or archive set exceeds 2 GiB")
    missing = [item["name"] for item, old in zip(expected["packages"], before) if old is None]
    if missing and not acknowledge:
        raise RuntimeError("Verified rollback packages are unavailable for " + ", ".join(missing) + "; check again and explicitly acknowledge updating without package rollback")

    def fetch(metadata):
        destination = os.path.join(directory, metadata["sha256"] + ".deb")
        if os.path.exists(destination):
            verify_archive(destination, metadata)
            return
        pkg = cache[metadata["name"] + ":" + metadata["architecture"]]
        version = next((v for v in pkg.versions if v.version == metadata["version"] and archive_metadata(v) == metadata), None)
        if version is None:
            raise RuntimeError("Authenticated package revision is no longer available: " + metadata["name"])
        # APT's file transport may return a symlink into a repository. Never
        # chmod/move that inode or retain a link. Copy bounded bytes into our own
        # root-owned inode, authenticate it, then publish the digest name.
        with tempfile.TemporaryDirectory(prefix=".fetch-", dir=directory) as download:
            path = version.fetch_binary(download, apt.progress.base.AcquireProgress(), allow_unauthenticated=False)
            fd, scratch = tempfile.mkstemp(prefix=".retained-", dir=directory)
            try:
                with open(path, "rb") as src, os.fdopen(fd, "wb") as dst:
                    remaining = metadata["size"] + 1
                    while remaining:
                        block = src.read(min(1024 * 1024, remaining))
                        if not block:
                            break
                        dst.write(block)
                        remaining -= len(block)
                    dst.flush()
                    os.fsync(dst.fileno())
                verify_archive(scratch, metadata)
                os.replace(scratch, destination)
                parent_fd = os.open(directory, os.O_DIRECTORY)
                try:
                    os.fsync(parent_fd)
                finally:
                    os.close(parent_fd)
            finally:
                if os.path.exists(scratch):
                    os.unlink(scratch)

    for metadata in after:
        emit(dict(phase="preparing_recovery", message="Retaining verified target package " + metadata["name"]))
        fetch(metadata)
    before_error = "Verified previous package revisions are unavailable: " + ", ".join(missing) if missing else ""
    if not missing:
        try:
            for metadata in before:
                emit(dict(phase="preparing_recovery", message="Retaining rollback package " + metadata["name"]))
                fetch(metadata)
        except Exception as error:
            if not acknowledge:
                raise RuntimeError("Could not retain the complete rollback set; no packages changed: " + str(error))
            before_error = "Could not retain the complete verified previous package set"
    manifest = dict(schema=1, version=expected["version"], before=before if not before_error else [],
                    after=after, rollback_available=not before_error, rollback_reason=before_error,
                    policy_hash=expected["policy_hash"], packages=expected["packages"])
    if native_bindings:
        verify_native_payloads(directory, after, native_bindings)
        if not before_error:
            verify_native_payloads(directory, before, native_bindings)
        manifest["native_bindings"] = native_bindings
    atomic_json(os.path.join(directory, "manifest.json"), manifest)
    emit(dict(archives=manifest))


def observe(cache, branch):
    packages = []
    unrelated = []
    ordinary_states = {apt_pkg.CURSTATE_NOT_INSTALLED, apt_pkg.CURSTATE_CONFIG_FILES, apt_pkg.CURSTATE_INSTALLED}
    for p in cache:
        matched = PATTERN.match(p.shortname)
        affected = matched and matched.group(1) == branch
        state = p._pkg.current_state
        if affected and (p.is_installed or state not in (apt_pkg.CURSTATE_NOT_INSTALLED, apt_pkg.CURSTATE_CONFIG_FILES)):
            packages.append(dict(name=p.shortname, architecture=p.installed.architecture if p.installed else p.architecture(),
                                 installed=p.installed.version if p.installed else "", state=state,
                                 automatic=p.is_auto_installed, held=p._pkg.selected_state == apt_pkg.SELSTATE_HOLD))
        elif not affected and state not in ordinary_states:
            unrelated.append(p.shortname)
    packages.sort(key=lambda p: (p["name"], p["architecture"]))
    return dict(version=branch, packages=packages, unrelated_pending=sorted(unrelated),
                journal_dirty=cache.dpkg_journal_dirty, policy_hash=policy_hash())


def load_manifest(directory, expected):
    private_directory(directory)
    path = os.path.join(directory, "manifest.json")
    info = os.lstat(path)
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size > 4 * 1024**2:
        raise RuntimeError("Unsafe recovery manifest")
    with open(path, encoding="utf-8") as source:
        manifest = json.load(source)
    if manifest != expected or manifest.get("schema") != 1:
        raise RuntimeError("Recovery manifest differs from the retained approved generation")
    return manifest


def recover_packages(cache, request):
    directory = request["directory"]
    manifest = load_manifest(directory, request["manifest"])
    goal = request["goal"]
    if goal not in ("before", "after") or goal == "before" and not manifest["rollback_available"]:
        raise RuntimeError("The requested package recovery set is unavailable")
    targets = manifest[goal]
    if not targets or len(targets) != len(manifest["packages"]):
        raise RuntimeError("The retained package set is incomplete")
    observation = observe(cache, manifest["version"])
    if observation["journal_dirty"] or observation["unrelated_pending"]:
        raise RuntimeError("Package state requires operator repair outside the retained branch transaction")
    if observation["policy_hash"] != manifest["policy_hash"]:
        raise RuntimeError("Repository policy changed since this update; reconcile it before recovery")
    original = {(p["name"], p["architecture"]): p for p in manifest["packages"]}
    allowed = {key: {p["installed"], p["candidate"]} for key, p in original.items()}
    actual = {(p["name"], p["architecture"]): p for p in observation["packages"]}
    if set(actual) != set(original):
        raise RuntimeError("The installed PHP package family changed since the recorded update")
    for key, p in actual.items():
        if p["installed"] not in allowed[key] or p["automatic"] != original[key]["automatic"] or p["held"] != original[key]["held"]:
            raise RuntimeError("Installed versions or package marks changed outside this update")
    expected_keys = set(original)
    if {(p["name"], p["architecture"]) for p in targets} != expected_keys:
        raise RuntimeError("Recovery archive identities differ from the installed family")
    for metadata in targets:
        if not re.fullmatch(r"[a-f0-9]{64}", metadata["sha256"]):
            raise RuntimeError("Invalid retained archive digest")
        verify_archive(os.path.join(directory, metadata["sha256"] + ".deb"), metadata)
    verify_native_payloads(directory, targets, manifest.get("native_bindings", []))

    # An operation-private, offline repository contains only already authenticated
    # retained files. The resolver still uses the real installed status and holds.
    index = []
    for p in targets:
        fields = dict(p["control"])
        fields.update(Package=p["name"], Version=p["version"], Architecture=p["architecture"],
                      Filename=p["sha256"] + ".deb", Size=str(p["size"]), SHA256=p["sha256"])
        index.append("\n".join(key + ": " + value for key, value in fields.items() if value))
    repo = os.path.join(directory, "recovery-" + goal)
    os.makedirs(repo, mode=0o700, exist_ok=True)
    private_directory(repo)
    # Avoid links: package bytes are copied and checked again in the private repo.
    for p in targets:
        destination = os.path.join(repo, p["sha256"] + ".deb")
        if not os.path.exists(destination):
            # The final digest name is never visible with partial contents.
            # Scratch names are private, unreferenced and safe to discard.
            fd, scratch = tempfile.mkstemp(prefix=".archive-", dir=repo)
            try:
                with os.fdopen(fd, "wb") as dst, open(os.path.join(directory, p["sha256"] + ".deb"), "rb") as src:
                    shutil.copyfileobj(src, dst)
                    dst.flush()
                    os.fsync(dst.fileno())
                verify_archive(scratch, p)
                os.replace(scratch, destination)
                parent_fd = os.open(repo, os.O_DIRECTORY)
                try:
                    os.fsync(parent_fd)
                finally:
                    os.close(parent_fd)
            finally:
                if os.path.exists(scratch):
                    os.unlink(scratch)
        verify_archive(destination, p)
    with open(os.path.join(repo, "Packages"), "w", encoding="utf-8") as target:
        target.write("\n\n".join(index) + "\n\n")
    sources = os.path.join(repo, "sources.list")
    with open(sources, "w", encoding="utf-8") as target:
        target.write("deb [trusted=yes] file:" + urllib.parse.quote(repo) + "/ ./\n")
    for subdirectory in ("lists", "lists/partial", "cache", "cache/archives", "cache/archives/partial"):
        os.makedirs(os.path.join(repo, subdirectory), mode=0o700, exist_ok=True)
    apt_pkg.config.set("Dir::Etc::sourcelist", sources)
    apt_pkg.config.set("Dir::Etc::sourceparts", "-")
    apt_pkg.config.set("Dir::State::lists", os.path.join(repo, "lists"))
    apt_pkg.config.set("Dir::Cache", os.path.join(repo, "cache"))
    apt_pkg.config.set("Dir::Cache::pkgcache", "")
    apt_pkg.config.set("Dir::Cache::srcpkgcache", "")
    apt_pkg.config.set("APT::Sandbox::User", "root")
    local = apt.Cache(apt.progress.base.OpProgress())
    if not local.update(apt.progress.base.AcquireProgress()):
        raise RuntimeError("Could not load the retained offline package set")
    local.open(apt.progress.base.OpProgress())
    local.clear()
    for p in targets:
        pkg = local[p["name"] + ":" + p["architecture"]]
        version = next((v for v in pkg.versions if v.version == p["version"] and v.record.get("SHA256", "").lower() == p["sha256"]), None)
        if version is None:
            raise RuntimeError("The offline package resolver cannot select the approved revision")
        if pkg._pkg.selected_state == apt_pkg.SELSTATE_HOLD:
            if not pkg.is_installed or pkg.installed.version != version.version or pkg._pkg.current_state != apt_pkg.CURSTATE_INSTALLED:
                raise RuntimeError("A held package prevents this recovery")
            continue
        pkg.candidate = version
        pkg.mark_install(auto_fix=False, auto_inst=False, from_user=False)
        if pkg.is_installed and pkg.installed.version == version.version:
            local._depcache.set_reinstall(pkg._pkg, True)
    if local.broken_count:
        raise RuntimeError("Retained PHP packages cannot satisfy the current dependencies")
    for pkg in local.get_changes():
        key = (pkg.shortname, pkg.candidate.architecture if pkg.candidate else "")
        target = next((p for p in targets if (p["name"], p["architecture"]) == key), None)
        if not target or pkg.marked_delete or not pkg.is_installed or not pkg.candidate or pkg.candidate.version != target["version"]:
            raise RuntimeError("The recovery resolver proposes changes outside the approved package set")
    emit(dict(phase="applying", message="Applying the verified retained PHP package set"))
    if not local.commit(apt.progress.base.AcquireProgress(), InstallProgress(), allow_unauthenticated=False):
        raise RuntimeError("The retained package transaction did not complete")
    local.open(apt.progress.base.OpProgress())
    for target in targets:
        pkg = local[target["name"] + ":" + target["architecture"]]
        if not pkg.is_installed or pkg.installed.version != target["version"] or pkg._pkg.current_state != apt_pkg.CURSTATE_INSTALLED or pkg.is_auto_installed != original[(target["name"], target["architecture"])]["automatic"]:
            raise RuntimeError("The recovered package set did not verify completely")
    if local.broken_count or local.dpkg_journal_dirty:
        raise RuntimeError("Package recovery left an unresolved package state")
    emit(dict(phase="packages_updated", message="Retained PHP package revisions verified"))


def inspect(cache, branch):
    family = []
    blocked = []
    for p in cache:
        m = PATTERN.match(p.shortname)
        if not m or m.group(1) != branch or not p.is_installed:
            continue
        if p._pkg.current_state != apt_pkg.CURSTATE_INSTALLED:
            blocked.append(p.shortname + " is not fully configured")
        family.append(package(p))
    family.sort(key=lambda p: (p["name"], p["architecture"]))
    names = {p["name"] for p in family}
    for required in ["fpm", "cli", "common"]:
        if "php" + branch + "-" + required not in names:
            blocked.append("PHP " + branch + " " + required + " package is not installed")
    native = apt_pkg.config.find("APT::Architecture")
    core_versions = set()
    changes = []
    for p in family:
        if p["architecture"] not in [native, "all"]:
            blocked.append("Foreign-architecture PHP packages require manual maintenance")
        if p["name"].endswith(("-cgi", "-phpdbg", "-dev")):
            blocked.append(p["name"] + " is outside the supported FPM/CLI runtime")
        if not p["candidate"]:
            blocked.append("No repository candidate for " + p["name"])
            continue
        # A dpkg-status-only candidate is not evidence that a repository has
        # published the currently installed revision (or future fixes).
        osources = p["origins"]
        if not osources or any(not o["trusted"] or o["origin"] not in ["Ubuntu", "UbuntuESM", "UbuntuESMApps", "LP-PPA-ondrej-php"] for o in osources):
            blocked.append("No usable trusted repository candidate for " + p["name"])
        cmp = apt_pkg.version_compare(p["candidate"], p["installed"])
        if cmp < 0:
            blocked.append("Candidate is older than installed " + p["name"])
        if p["source"] == "php" + branch:
            core_versions.add(p["candidate_source_version"])
        if cmp <= 0:
            continue
        if p["held"]:
            blocked.append(p["name"] + " is held")
        if p["source"] != p["candidate_source"]:
            blocked.append(p["name"] + " changes source package")
        osources = p["origins"]
        if not osources or any(not o["trusted"] or o["origin"] not in ["Ubuntu", "UbuntuESM", "UbuntuESMApps", "LP-PPA-ondrej-php"] for o in osources):
            blocked.append("Unrecognized or untrusted origin for " + p["name"])
        # Historical repository origin is not retained by dpkg. Require an
        # identifiable distro/PPA revision family; never claim origin proof.
        is_ppa = any(o["origin"] == "LP-PPA-ondrej-php" for o in osources)
        if is_ppa != ("deb.sury.org" in p["installed"]):
            blocked.append("Cannot establish repository continuity for " + p["name"])
        if not re.fullmatch(r"[a-fA-F0-9]{64}", p["sha256"]):
            blocked.append("Missing SHA256 metadata for " + p["name"])
        changes.append(p)
    if len(core_versions) > 1:
        blocked.append("PHP core package candidates are not a consistent source revision")
    # No dependency auto-installation or automatic broken-package repair.
    cache.clear()
    for p in changes:
        pkg = cache[p["name"] + ":" + p["architecture"]]
        pkg.mark_install(auto_fix=False, auto_inst=False, from_user=False)
    if cache.broken_count:
        blocked.append("The update requires dependency changes outside this package family")
    actual = cache.get_changes()
    intended = {(p["name"], p["architecture"], p["candidate"]) for p in changes}
    resolved = set()
    for p in actual:
        if not p.is_installed or p.marked_delete or not p.candidate or not p.marked_upgrade:
            blocked.append("The resolver proposes an unsupported package operation")
        else:
            resolved.add((p.shortname, p.candidate.architecture, p.candidate.version))
    if intended != resolved:
        blocked.append("Resolved package changes differ from the PHP update plan")
    return dict(version=branch, packages=family, changes=changes,
                blocked_reasons=sorted(set(blocked)), policy_hash=policy_hash())


@contextlib.contextmanager
def system_lock():
    deadline = time.monotonic() + 120
    while True:
        try:
            apt_pkg.pkgsystem_lock()
            break
        except SystemError as error:
            # RunMaintenance fixes LC_ALL=C.UTF-8. libapt refuses this lock
            # for an interrupted dpkg journal too; waiting cannot repair it.
            if "dpkg was interrupted" in str(error):
                raise RuntimeError("The interrupted package journal requires operator repair before scoped PHP maintenance") from error
            if time.monotonic() >= deadline:
                raise RuntimeError("Package manager is busy; retry after its current operation finishes")
            time.sleep(1)
    try:
        yield
    finally:
        apt_pkg.pkgsystem_unlock()


class InstallProgress(apt.progress.base.InstallProgress):
    def status_change(self, pkg, percent, status):
        emit(dict(phase="applying", message=status, percent=percent))

    def error(self, pkg, error):
        emit(dict(phase="applying", message=pkg + ": " + error))

    def conffile(self, current, new):
        # --force-confold is configured; never wait for an interactive answer.
        pass


def main():
    request = json.load(sys.stdin)
    action = request["action"]
    if action == "verify_archives":
        # This authenticates immutable retained files only. It neither reads
        # the live package database nor needs its lock, which may be unavailable
        # after an interrupted journal. Mutation still always takes that lock.
        manifest = load_manifest(request["directory"], request["manifest"])
        goal = request["goal"]
        if goal not in ("before", "after") or goal == "before" and not manifest["rollback_available"]:
            raise RuntimeError("The retained package target is unavailable")
        targets = manifest[goal]
        if not targets or len(targets) != len(manifest["packages"]):
            raise RuntimeError("The retained package set is incomplete")
        for metadata in targets:
            if not re.fullmatch(r"[a-f0-9]{64}", metadata["sha256"]):
                raise RuntimeError("Invalid retained package identity")
            verify_archive(os.path.join(request["directory"], metadata["sha256"] + ".deb"), metadata)
        verify_native_payloads(request["directory"], targets, manifest.get("native_bindings", []))
        emit(dict(archives=manifest))
        return
    with system_lock():
        cache = apt.Cache(apt.progress.base.OpProgress())
        if action == "observe":
            emit(dict(observation=observe(cache, request["version"])))
            return
        if action == "recover":
            recover_packages(cache, request)
            return
        if cache.dpkg_journal_dirty or cache.broken_count:
            raise RuntimeError("The package database requires operator repair before PHP maintenance")
        if action == "inspect":
            branches = sorted({m.group(1) for p in cache if p.is_installed
                               for m in [PATTERN.match(p.shortname)] if m
                               and p.shortname == "php" + m.group(1) + "-fpm"})
            emit(dict(branches=[inspect(cache, b) for b in branches]))
            return
        if action not in ("apply", "prepare"):
            raise RuntimeError("Unsupported package action")
        expected = request["inventory"]
        actual = inspect(cache, expected["version"])
        if actual != expected or actual["blocked_reasons"] or not actual["changes"]:
            raise RuntimeError("PHP package plan changed; check for updates again")
        if action == "prepare":
            prepare_archives(cache, expected, request["directory"], request.get("acknowledge_no_rollback") is True, request.get("native_bindings", []))
            return
        # The cache was opened and resolved while holding the native frontend
        # lock. commit nests that lock and only releases the inner dpkg lock
        # when handing control to dpkg, preserving transaction ownership.
        emit(dict(phase="downloading", message="Downloading and verifying PHP packages"))
        if not cache.commit(apt.progress.base.AcquireProgress(), InstallProgress(), allow_unauthenticated=False):
            raise RuntimeError("APT did not complete the PHP package transaction")
        cache.open(apt.progress.base.OpProgress())
        for p in expected["packages"]:
            item = cache[p["name"] + ":" + p["architecture"]]
            target = p["candidate"] if p in expected["changes"] else p["installed"]
            if not item.is_installed or item.installed.version != target or item.is_auto_installed != p["automatic"] or item._pkg.current_state != apt_pkg.CURSTATE_INSTALLED:
                raise RuntimeError("Installed package revisions or automatic marks differ from the approved transaction")
        if cache.broken_count or cache.dpkg_journal_dirty:
            raise RuntimeError("The package database requires repair after the update")
        emit(dict(phase="packages_updated", message="PHP package revisions verified"))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        emit(dict(error=str(error)))
        sys.exit(1)
