"""Bounded Ubuntu PHP repository review and journaled source migration.

Uses the fixed apt adapter's primitives. Only trusted server code supplies paths;
HTTP callers select a stored plan ID, never repository URLs, keys or filenames.
"""
import base64
import shlex
import subprocess

REPO_URL = "https://packages.sury.org/php/"
REPO_KEY = "/usr/share/keyrings/fluxo-sury-archive.gpg"
REPO_SOURCE = "/etc/apt/sources.list.d/fluxo-sury-php.sources"
REPO_PREFERENCES = "/etc/apt/preferences.d/fluxo-php-vendors"
REPO_FINGERPRINT = "15058500A0235D97F5D10063B188E2B695BD4743"
REPO_KEY_SHA = "b486fd5488185c4c46467960fa69c53d5085fec492cf76b9eaf3db33561c9d7c"
PPA_HOSTS = {"ppa.launchpadcontent.net", "ppa.launchpad.net"}
NATIVE_ORIGINS = ("Ubuntu", "UbuntuESM", "UbuntuESMApps")


def repo_fields(block):
    # TagSection parses RFC822 fields but does not discard APT comment lines.
    cleaned = "\n".join(line for line in block.splitlines() if not line.lstrip().startswith("#"))
    names = [m.group(1).lower() for line in cleaned.splitlines() for m in [re.match(r"^([A-Za-z][A-Za-z0-9-]*):", line)] if m]
    if len(names) != len(set(names)):
        raise RuntimeError("Duplicate repository policy fields require manual reconciliation")
    return apt_pkg.TagSection(cleaned.rstrip("\n") + "\n")


def repo_native_revision(version):
    return not sury_revision(version) and bool(re.search(r"ubuntu[0-9.]*(?:\+esm[0-9]+)?$", version))


def repo_native_pin(branch):
    pin = "".join(f"Package: php{branch} php{branch}-*\nPin: release o={origin}\nPin-Priority: 990\n\n" for origin in NATIVE_ORIGINS)
    # Without this exclusion, APT may fall back to a newer vendor build when
    # every available native revision is older than the installed revision.
    return pin + "".join(f"Package: php{branch} php{branch}-*\nPin: release o={origin}\nPin-Priority: -1\n\n" for origin in ("deb.sury.org", "LP-PPA-ondrej-php"))


def repo_pin_overlap(packages, branches):
    for token in packages.split():
        # APT source/regex/pattern selectors require operator interpretation.
        if token.startswith(("src:", "/", "?", "~")):
            return True
        token = token.split(":", 1)[0]
        prefix = re.split(r"[?*\[]", token, 1)[0]
        if any(prefix.startswith("php" + b) or ("php" + b).startswith(prefix) for b in branches):
            return True
    return False


def repo_check_custom_pins(branches):
    if not branches:
        return
    paths = ["/etc/apt/preferences"] + ["/etc/apt/preferences.d/" + n for n in os.listdir("/etc/apt/preferences.d") if re.fullmatch(r"[A-Za-z0-9_.-]+", n)]
    for path in paths:
        if path == REPO_PREFERENCES:
            continue
        raw = repo_file(path, missing=True)
        if raw is None:
            continue
        for block in re.split(r"\n\s*\n", raw.decode()):
            if not any(line.strip() and not line.lstrip().startswith("#") for line in block.splitlines()):
                continue
            fields = repo_fields(block)
            # Ubuntu Pro ships these positive, generic ESM preferences on
            # ordinary Noble hosts too. They express no package exclusion;
            # preserve the files while applying our native-family priority.
            if (len(fields.keys()) == 3 and fields.get("Package") == "*"
                    and fields.get("Pin", "").split() in (["release", "o=UbuntuESM"], ["release", "o=UbuntuESMApps"])
                    and fields.get("Pin-Priority", "").strip() == "510"):
                continue
            if not fields.get("Package") or repo_pin_overlap(fields["Package"], branches):
                raise RuntimeError("Custom PHP package priorities require manual reconciliation before repository setup: " + path)


def repo_file(path, missing=False):
    current = path
    while current != "/":
        if not os.path.lexists(current) and missing and current == path:
            current = os.path.dirname(current)
            continue
        st = os.lstat(current)
        if stat.S_ISLNK(st.st_mode) or st.st_uid != 0 or st.st_mode & 0o022:
            raise RuntimeError("Unsafe repository configuration path: " + path)
        current = os.path.dirname(current)
    if not os.path.exists(path) and missing:
        return None
    st = os.lstat(path)
    if not stat.S_ISREG(st.st_mode) or st.st_nlink != 1 or st.st_size > 1024**2:
        raise RuntimeError("Unsupported repository configuration file: " + path)
    with open(path, "rb") as f:
        return f.read()


def repo_platform():
    values = {}
    for line in repo_file(os.path.realpath("/etc/os-release")).decode().splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            values[key] = value.strip('"')
    codename = values.get("VERSION_CODENAME")
    architecture = apt_pkg.config.find("APT::Architecture")
    if values.get("ID") != "ubuntu" or codename not in {"jammy", "noble", "resolute"} or architecture not in {"amd64", "arm64"}:
        raise RuntimeError("Managed PHP repository setup supports Ubuntu 22.04, 24.04 and 26.04 on amd64 or arm64")
    return codename, architecture


def repo_kind(uri):
    u = urllib.parse.urlsplit(uri)
    if u.username or u.password or u.query or u.fragment:
        return "custom"
    if u.hostname in PPA_HOSTS and u.path.rstrip("/") == "/ondrej/php/ubuntu" and u.scheme in {"http", "https"}:
        return "ppa"
    if u.hostname == "packages.sury.org" and u.path.rstrip("/") == "/php" and u.scheme == "https" and u.port in {None, 443}:
        return "sury"
    return "other"


def repo_rewrite(path, data, codename):
    text = data.decode("utf-8")
    ppa, sury = 0, 0
    if path.endswith(".sources"):
        output = []
        for block in re.split(r"(\n\s*\n)", text):
            if not block.strip() or not any(line.strip() and not line.lstrip().startswith("#") for line in block.splitlines()):
                output.append(block)
                continue
            fields = repo_fields(block)
            names = [m.group(1).lower() for line in block.splitlines() for m in [re.match(r"^([A-Za-z][A-Za-z0-9-]*):", line)] if m]
            if len(names) != len(set(names)):
                raise RuntimeError("Review duplicate PHP repository fields manually: " + path)
            uris = fields.get("URIs", "").split()
            kinds = {repo_kind(u) for u in uris}
            if not kinds & {"ppa", "sury"} or fields.get("Enabled", "yes").lower() == "no":
                output.append(block)
                continue
            if len(uris) != 1 or len(kinds) != 1 or fields.get("Suites", "").split() != [codename] or fields.get("Components", "").split() != ["main"] or set(fields.get("Types", "").split()) - {"deb", "deb-src"}:
                raise RuntimeError("Review mixed PHP repository stanza manually: " + path)
            allowed = {"Types", "URIs", "Suites", "Components", "Signed-By", "Enabled", "Architectures"}
            if {name.lower() for name in fields.keys()} - {name.lower() for name in allowed}:
                raise RuntimeError("Review custom PHP repository options manually: " + path)
            if kinds == {"sury"}:
                if fields.get("Signed-By") != REPO_KEY:
                    raise RuntimeError("Existing Sury source uses another signing-key policy; reconcile it manually: " + path)
                sury += 1
                output.append(block)
            else:
                ppa += 1
                # Preserve every byte, including comments, except this field.
                if "enabled" in names:
                    block = re.sub(r"(?im)^enabled:.*$", "Enabled: no", block)
                else:
                    block = block.rstrip("\n") + "\nEnabled: no\n"
                output.append(block)
        return "".join(output).encode(), ppa, sury
    output = []
    for line in text.splitlines(keepends=True):
        if line.lstrip().startswith("#") or not line.strip():
            output.append(line)
            continue
        tokens = shlex.split(line, comments=True)
        relevant = [t for t in tokens if repo_kind(t) in {"ppa", "sury"}]
        if not relevant:
            output.append(line)
            continue
        uri = relevant[0]
        index = tokens.index(uri)
        if len(relevant) != 1 or tokens[0] not in {"deb", "deb-src"} or tokens[index + 1:] != [codename, "main"]:
            raise RuntimeError("Review custom PHP source manually: " + path)
        options = " ".join(tokens[1:index])
        if options and not re.fullmatch(r"\[(?:(?:arch=[a-z0-9,]+|signed-by=/[a-zA-Z0-9/_.-]+)\s*)+\]", options):
            raise RuntimeError("Review custom PHP source options manually: " + path)
        if repo_kind(uri) == "ppa":
            ppa += 1
            output.append("# Disabled by Fluxo PHP repository migration: " + line)
        else:
            if "signed-by=" + REPO_KEY not in options:
                raise RuntimeError("Existing Sury source uses another signing-key policy; reconcile it manually: " + path)
            sury += 1
            output.append(line)
    return "".join(output).encode(), ppa, sury


def repo_proposed_policy(changes):
    edits = {item["path"]: base64.b64decode(item["after"]) for item in changes}
    digest = hashlib.sha256()
    for base in ["/etc/apt/sources.list", "/etc/apt/sources.list.d", "/etc/apt/preferences", "/etc/apt/preferences.d"]:
        paths = [base]
        if os.path.isdir(base):
            paths = sorted({os.path.join(base, name) for name in os.listdir(base)} | {p for p in edits if os.path.dirname(p) == base})
        for path in paths:
            if path in edits or os.path.isfile(path):
                digest.update(path.encode())
                digest.update(edits[path] if path in edits else repo_file(path))
    return digest.hexdigest()


def repo_configuration():
    codename, architecture = repo_platform()
    files = {}
    sources = ["/etc/apt/sources.list"]
    for name in sorted(os.listdir("/etc/apt/sources.list.d")):
        if name.endswith((".list", ".sources")):
            sources.append("/etc/apt/sources.list.d/" + name)
    for path in sources:
        raw = repo_file(path, missing=True)
        if raw is not None:
            files[path] = raw
    changes, ppa, sury = [], 0, 0
    for path, raw in files.items():
        after, old, new = repo_rewrite(path, raw, codename)
        ppa += old
        sury += new
        if raw != after:
            changes.append(dict(path=path, before=base64.b64encode(raw).decode(), after=base64.b64encode(after).decode(), **repo_metadata(path)))
    if sury > 1:
        raise RuntimeError("Duplicate active Sury PHP sources require manual reconciliation")
    if not sury:
        if REPO_SOURCE in files:
            raise RuntimeError("The managed Sury source path is already occupied; reconcile it manually")
        after = f"Types: deb\nURIs: {REPO_URL}\nSuites: {codename}\nComponents: main\nArchitectures: {architecture}\nSigned-By: {REPO_KEY}\n"
        changes.append(dict(path=REPO_SOURCE, before=None, after=base64.b64encode(after.encode()).decode(), before_mode=None, before_uid=None, before_gid=None))
    native_branches = []
    cache = apt.Cache(apt.progress.base.OpProgress())
    for pkg in cache:
        match = PATTERN.match(pkg.shortname)
        if (pkg.is_installed and match and pkg.shortname == "php" + match.group(1) + "-fpm"
                and pkg.installed.source_name == "php" + match.group(1)
                and repo_native_revision(pkg.installed.version)):
            native_branches.append(match.group(1))
    pin = "# Managed by Fluxo: keep installed Ubuntu PHP branches on Ubuntu. No forced downgrades.\n"
    repo_check_custom_pins(native_branches)
    for branch in sorted(native_branches):
        pin += repo_native_pin(branch)
    previous_pin = repo_file(REPO_PREFERENCES, missing=True)
    if previous_pin is not None and not previous_pin.startswith(b"# Managed by Fluxo: keep installed Ubuntu PHP branches on Ubuntu."):
        raise RuntimeError("The managed PHP priority path contains custom configuration; reconcile it manually")
    if native_branches or previous_pin is not None:
        after_pin = pin.encode()
        if previous_pin != after_pin:
            changes.append(dict(path=REPO_PREFERENCES, before=base64.b64encode(previous_pin).decode() if previous_pin is not None else None,
                                after=base64.b64encode(after_pin).decode(), **(repo_metadata(REPO_PREFERENCES) if previous_pin is not None else dict(before_mode=None, before_uid=None, before_gid=None))))
    key = repo_file(REPO_KEY, missing=True)
    if key is not None and hashlib.sha256(key).hexdigest() != REPO_KEY_SHA:
        raise RuntimeError("The managed Sury key path contains an unexpected key")
    # Hash all source/pin policy bytes, including unrelated sources. The existing
    # adapter includes them in package/recovery identity too.
    configuration = dict(codename=codename, architecture=architecture, policy_hash=policy_hash(),
                         key_hash=hashlib.sha256(key).hexdigest() if key else "", key_metadata=repo_metadata(REPO_KEY) if key else None, changes=changes,
                         ppa_entries=ppa, sury_entries=sury, key_fingerprint=REPO_FINGERPRINT)
    configuration["after_policy"] = repo_proposed_policy(changes)
    configuration["configuration_id"] = hashlib.sha256(json.dumps(configuration, sort_keys=True).encode()).hexdigest()
    return configuration


def repo_metadata(path):
    info = os.lstat(path)
    return dict(before_mode=stat.S_IMODE(info.st_mode), before_uid=info.st_uid, before_gid=info.st_gid)


def repo_matches(item, data):
    current = repo_file(item["path"], missing=True)
    if current != data:
        return False
    if current is None:
        return True
    info = os.lstat(item["path"])
    expected_mode = item.get("before_mode") if item.get("before") is not None else 0o644
    expected_uid = item.get("before_uid") if item.get("before") is not None else 0
    expected_gid = item.get("before_gid") if item.get("before") is not None else 0
    return (stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid) == (expected_mode, expected_uid, expected_gid)


def repo_sync(path, value, mode=0o644, uid=0, gid=0):
    repo_file(path, missing=True)
    if value is None:
        if os.path.exists(path):
            os.unlink(path)
    else:
        fd, temp = tempfile.mkstemp(prefix=".fluxo-repository-", dir=os.path.dirname(path))
        try:
            with os.fdopen(fd, "wb") as f:
                os.fchown(f.fileno(), uid, gid)
                os.fchmod(f.fileno(), mode)
                f.write(value)
                f.flush()
                os.fsync(f.fileno())
            os.replace(temp, path)
        finally:
            if os.path.exists(temp):
                os.unlink(temp)
    fd = os.open(os.path.dirname(path), os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def repo_refresh(args, deadline=240):
    subprocess.run(["/usr/bin/apt-get", "-o", "APT::Update::Error-Mode=any", "-o", "DPkg::Lock::Timeout=120",
                    "-o", "Acquire::http::Timeout=25", "-o", "Acquire::https::Timeout=25", *args, "update"],
                   check=True, timeout=deadline)


def repo_stage(plan, directory, key):
    sources = os.path.join(directory, "sources")
    lists = os.path.join(directory, "lists")
    os.mkdir(sources)
    os.mkdir(lists)
    os.chmod(directory, 0o755)
    key_path = os.path.join(directory, "sury.gpg")
    repo_sync(key_path, key)
    edits = {c["path"]: base64.b64decode(c["after"]) for c in plan["changes"]}
    main_path = os.path.join(directory, "sources.list")
    repo_sync(main_path, b"")
    original = ["/etc/apt/sources.list"] + ["/etc/apt/sources.list.d/" + n for n in os.listdir("/etc/apt/sources.list.d") if n.endswith((".list", ".sources"))]
    for path in sorted(set(original) | {p for p in edits if p == "/etc/apt/sources.list" or p.startswith("/etc/apt/sources.list.d/")}):
        data = edits.get(path, repo_file(path, missing=True))
        if data is None:
            continue
        # Only the managed key reference changes in this private staged copy.
        data = data.replace(REPO_KEY.encode(), key_path.encode())
        target = main_path if path == "/etc/apt/sources.list" else os.path.join(sources, os.path.basename(path))
        repo_sync(target, data)
    preferences = os.path.join(directory, "preferences")
    os.mkdir(preferences)
    preference_paths = {"/etc/apt/preferences.d/" + name for name in os.listdir("/etc/apt/preferences.d")}
    preference_paths |= {p for p in edits if p.startswith("/etc/apt/preferences.d/")}
    for path in sorted(preference_paths):
        data = edits.get(path, repo_file(path, missing=True))
        if data is not None:
            repo_sync(os.path.join(preferences, os.path.basename(path)), data)
    args = ["-o", "Dir::Etc::sourcelist=" + main_path, "-o", "Dir::Etc::sourceparts=" + sources,
            "-o", "Dir::State::lists=" + lists, "-o", "Dir::Cache::pkgcache=", "-o", "Dir::Cache::srcpkgcache=",
            "-o", "APT::Sandbox::User=root", "-o", "Dir::Etc::preferencesparts=" + preferences]
    repo_refresh(args)
    return args


def repo_restore(journal, key):
    # First validate the entire set, then restore. Never overwrite operator edits.
    files = journal["files"]
    for item in files:
        current = repo_file(item["path"], missing=True)
        before = base64.b64decode(item["before"]) if item["before"] is not None else None
        after = base64.b64decode(item["after"])
        if not repo_matches(item, before) and not repo_matches(item, after):
            raise RuntimeError("Repository recovery conflicts with an operator edit: " + item["path"])
    for item in reversed(files):
        repo_sync(item["path"], base64.b64decode(item["before"]) if item["before"] is not None else None,
                  (0o644 if item.get("before_mode") is None else item["before_mode"]), item.get("before_uid") or 0, item.get("before_gid") or 0)


def repo_inventory(cache):
    return sorted([dict(name=p.shortname, architecture=p.installed.architecture,
                        version=p.installed.version, source=p.installed.source_name,
                        state=p._pkg.current_state, held=p._pkg.selected_state == apt_pkg.SELSTATE_HOLD,
                        automatic=p.is_auto_installed) for p in cache if p.is_installed and PATTERN.match(p.shortname)],
                  key=lambda p: (p["name"], p["architecture"]))


def repo_retain(directory):
    retained = os.path.join(directory, "retained")
    os.makedirs(retained, mode=0o700, exist_ok=True)
    private_directory(retained)
    load_retained_catalog(retained)
    catalog = list(RETAINED_CATALOG)
    missing = []
    with system_lock():
        cache = apt.Cache(apt.progress.base.OpProgress())
        wanted = [p for p in cache if p.is_installed and PATTERN.match(p.shortname)]
        total = sum(p.installed.size for p in wanted)
        if total > 2 * 1024**3 or shutil.disk_usage(retained).free < total * 3 + 256 * 1024**2:
            raise RuntimeError("Insufficient storage to preserve installed PHP rollback packages")
        for pkg in wanted:
            metadata = archive_metadata(pkg.installed)
            if not metadata:
                missing.append(pkg.shortname)
                continue
            destination = os.path.join(retained, metadata["sha256"] + ".deb")
            try:
                if os.path.exists(destination):
                    verify_archive(destination, metadata)
                else:
                    cached = []
                    archive_cache = "/var/cache/apt/archives"
                    for name in os.listdir(archive_cache):
                        if name.startswith(metadata["name"] + "_") and name.endswith("_" + metadata["architecture"] + ".deb"):
                            path = os.path.join(archive_cache, name)
                            try:
                                verify_archive(path, metadata)
                                cached.append(path)
                            except Exception:
                                pass
                    with tempfile.TemporaryDirectory(prefix="fetch-", dir=retained) as scratch:
                        path = cached[0] if cached else pkg.installed.fetch_binary(scratch, apt.progress.base.AcquireProgress(), allow_unauthenticated=False)
                        with open(path, "rb") as source:
                            data = source.read(metadata["size"] + 1)
                        repo_sync(destination, data, 0o600)
                        verify_archive(destination, metadata)
                if metadata not in catalog:
                    catalog.append(metadata)
            except Exception:
                missing.append(pkg.shortname)
        atomic_json(os.path.join(retained, "catalog.json"), catalog)
    load_retained_catalog(retained)
    return sorted(set(missing))


def repo_authorize(directory, journal):
    atomic_json(os.path.join(directory, "authorization.json"),
                dict(policy_hash=journal["after_policy"], key_sha256=REPO_KEY_SHA))


def repository_main(request):
    action = request["action"]
    if action == "repository_status":
        emit(dict(repository=repo_configuration()))
        return
    directory = request["directory"]
    private_directory(directory)
    key = base64.b64decode(request["key"])
    if hashlib.sha256(key).hexdigest() != REPO_KEY_SHA:
        raise RuntimeError("Embedded PHP repository key does not match the pinned identity")
    if action == "repository_review":
        plan = repo_configuration()
        with system_lock():
            plan["package_inventory"] = repo_inventory(apt.Cache(apt.progress.base.OpProgress()))
        plan["rollback_unavailable"] = repo_retain(directory)
        with tempfile.TemporaryDirectory(prefix="stage-", dir=directory) as stage:
            args = repo_stage(plan, stage, key)
            # Inspect exactly the authenticated staged policy without installing
            # packages. Existing custom pins remain effective.
            for i in range(0, len(args), 2):
                setting, value = args[i + 1].split("=", 1)
                apt_pkg.config.set(setting, value)
            global REPOSITORY_AUTHORIZED
            REPOSITORY_AUTHORIZED = True
            with system_lock():
                cache = apt.Cache(apt.progress.base.OpProgress())
                branches = sorted({m.group(1) for p in cache if p.is_installed for m in [PATTERN.match(p.shortname)] if m and p.shortname == "php" + m.group(1) + "-fpm"})
                inventories = [inspect(cache, b) for b in branches]
                plan["package_blockers"] = {inv["version"]: inv["blocked_reasons"] for inv in inventories}
                plan["retained_extensions"] = sorted(p["name"] for inv in inventories for p in inv["packages"] if p.get("preserved_extension"))
                plan["migration_blockers"] = sorted("Cannot preserve authenticated installed extension " + p["name"] + "; reconcile it before switching repositories" for inv in inventories for p in inv["packages"] if p["source"] != "php" + inv["version"] and not p["origins"] and not p.get("preserved_extension"))
                if repo_inventory(cache) != plan["package_inventory"]:
                    raise RuntimeError("Installed PHP packages changed during repository review; review again")
                if cache.broken_count or cache.dpkg_journal_dirty:
                    raise RuntimeError("The package database needs operator repair before repository setup")
        if repo_configuration()["configuration_id"] != plan["configuration_id"]:
            raise RuntimeError("Repository configuration changed during review")
        emit(dict(repository=plan))
        return
    if action != "repository_apply":
        raise RuntimeError("Unsupported repository action")
    with system_lock():
        repository_apply(request, directory, key)


def repository_apply(request, directory, key):
    plan = request["plan"]
    journal_path = os.path.join(directory, "journal.json")
    if os.path.exists(journal_path):
        journal = json.loads(repo_file(journal_path).decode())
        if journal["plan_id"] != request["plan_id"]:
            if journal["state"] == "pending":
                raise RuntimeError("Resume the previous repository setup before starting another")
            journal = None
        elif journal["state"] == "committed":
            if policy_hash() != journal["after_policy"]:
                raise RuntimeError("Repository policy changed after setup; manual reconciliation required")
            repo_authorize(directory, journal)
            emit(dict(repository=journal))
            return
        elif journal["state"] == "pending":
            repo_restore(journal, key)
            journal["state"] = "restored"
            atomic_json(journal_path, journal)
    if repo_configuration()["configuration_id"] != plan["configuration_id"]:
        raise RuntimeError("Repository review expired because configuration changed; review again")
    cache = apt.Cache(apt.progress.base.OpProgress())
    if repo_inventory(cache) != plan["package_inventory"]:
        raise RuntimeError("Installed PHP packages or marks changed since repository review; review again")
    if plan.get("migration_blockers"):
        raise RuntimeError("; ".join(plan["migration_blockers"]))
    package_state = hashlib.sha256(open("/var/lib/dpkg/status", "rb").read()).hexdigest()
    files = [dict(path=REPO_KEY, before=base64.b64encode(repo_file(REPO_KEY)).decode() if os.path.exists(REPO_KEY) else None,
                  after=base64.b64encode(key).decode(), **(repo_metadata(REPO_KEY) if os.path.exists(REPO_KEY) else dict(before_mode=None, before_uid=None, before_gid=None)))] + plan["changes"]
    journal = dict(plan_id=request["plan_id"], state="pending", before_policy=plan["policy_hash"], files=files)
    atomic_json(journal_path, journal)
    try:
        for item in files:
            before = base64.b64decode(item["before"]) if item["before"] is not None else None
            if not repo_matches(item, before):
                raise RuntimeError("Repository configuration changed before publication: " + item["path"])
            repo_sync(item["path"], base64.b64decode(item["after"]), (0o644 if item.get("before_mode") is None else item["before_mode"]), item.get("before_uid") or 0, item.get("before_gid") or 0)
        repo_refresh([])
        current = repo_configuration()
        if current["ppa_entries"] or current["sury_entries"] != 1:
            raise RuntimeError("Repository publication did not establish one active Sury source")
        if hashlib.sha256(open("/var/lib/dpkg/status", "rb").read()).hexdigest() != package_state:
            raise RuntimeError("Package state changed during repository setup")
        if policy_hash() != plan["after_policy"]:
            raise RuntimeError("Repository policy changed concurrently with publication")
        journal["after_policy"] = policy_hash()
        journal["state"] = "committed"
        atomic_json(journal_path, journal)
    except Exception:
        repo_restore(journal, key)
        journal["state"] = "restored"
        atomic_json(journal_path, journal)
        raise
    repo_authorize(directory, journal)
    emit(dict(repository=journal))
