"""Probe an unchanged public release on an explicitly opted-in disposable ARM runner.

This controller never rebuilds Fluxo, changes its architecture gates, supplies a
local binary, changes journal authority, or substitutes cron receipts. A hosted
runner is not a pristine VPS: failures are evidence, not blanket ARM acceptance.
Raw installer output stays private; only sanitized output and metadata are kept.
"""

import hashlib
import json
import os
from pathlib import Path
import platform
import pwd
import re
import ssl
import stat
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


TAG = "v0.4.47"
EXPECTED_BINARY = "621f162b43e481f777929b8f280800cd4e9837facf6414545be317563bedc621"
EXPECTED_INSTALLER = "d5830d050b14b6209f28a74a5da06a0deb040c8025e0fe27b654d3423656c16d"
BASE_URL = "https://github.com/FabioTECH1/fluxo/releases/download/" + TAG
SECRET_LINE = re.compile(r"password|token|credential|secret|authorization|private.key|bootstrap", re.I)


def run(args, timeout=30, env=None):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout, env=env)
    return {"exit_code": result.returncode, "stdout": result.stdout.strip(), "stderr": result.stderr.strip()}


def sanitized(text):
    return "\n".join("[redacted secret-bearing line]" if SECRET_LINE.search(line) else line for line in text.splitlines())


def digest(path):
    with open(path, "rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def download(name, target, expected):
    result = run(["curl", "-fSL", "--retry", "2", "--max-time", "180", BASE_URL + "/" + name, "-o", str(target)], timeout=570)
    if result["exit_code"] != 0:
        raise RuntimeError("public download failed for " + name + ": " + sanitized(result["stderr"]))
    if digest(target) != expected:
        raise RuntimeError("public asset checksum mismatch for " + name)


def state_summary(path):
    if not path.exists():
        return {"exists": False}
    raw = path.read_bytes()
    value = json.loads(raw)
    result = {"exists": True, "sha256": hashlib.sha256(raw).hexdigest()}
    for key in ["schema", "phase", "scheduler_schema", "system_task_schema", "loader_epochs_established"]:
        if key in value:
            result[key] = value[key]
    if "entries" in value:
        entries = list(value["entries"].values())
        result["entry_count"] = len(entries)
        result["enabled_count"] = sum(bool(e.get("enabled")) for e in entries)
        result["received_count"] = sum(bool(e.get("received")) for e in entries)
    result["invocation_count"] = len(value.get("invocations", {}))
    return result


def runner_system_paths():
    """Record image metadata; secure only root-owned real system directories.

    This is disposable fixture preparation, not an installer or guard change.
    Non-root ownership, symlinks and unexpected types are left for refusal.
    """
    result = []
    for name in ["/usr", "/usr/share", "/usr/share/keyrings", "/usr/local", "/usr/local/bin", "/usr/local/sbin", "/etc", "/etc/apt", "/etc/apt/sources.list.d", "/etc/apt/preferences.d"]:
        path = Path(name)
        if not path.exists():
            result.append({"path": name, "exists": False})
            continue
        before = path.lstat()
        item = {"path": name, "uid": before.st_uid, "gid": before.st_gid, "mode_before": oct(stat.S_IMODE(before.st_mode)), "symlink": stat.S_ISLNK(before.st_mode)}
        if stat.S_ISDIR(before.st_mode) and before.st_uid == 0 and before.st_mode & 0o022:
            path.chmod(stat.S_IMODE(before.st_mode) & ~0o022)
            item["fixture_permissions_hardened"] = True
        item["mode_after"] = oct(stat.S_IMODE(path.lstat().st_mode))
        result.append(item)
    return result


def api(base, method, path, data=None, bearer=None):
    headers = {"Content-Type": "application/json"}
    if bearer:
        headers["Authorization"] = "Bearer " + bearer
    request = urllib.request.Request(base + path, data=json.dumps(data).encode() if data is not None else None, method=method, headers=headers)
    try:
        with urllib.request.urlopen(request, context=ssl._create_unverified_context(), timeout=60) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        return error.code, sanitized(error.read().decode(errors="replace"))


def basic_api_checks(base, data_dir):
    # Read only our newly generated bootstrap credential; never report it/JWTs.
    credentials = (data_dir / ".fluxo_credentials").read_text()
    match = re.search(r"^Fluxo bootstrap token[^:]*:\s*([0-9a-f]{32})\s*$", credentials, re.M)
    if not match:
        return {"passed": False, "reason": "new fixture bootstrap credential unavailable"}
    status, result = api(base, "POST", "/api/v1/auth/login", {"username": "arm64-probe-admin", "password": match.group(1)})
    if status != 200 or not isinstance(result, dict) or not result.get("token"):
        return {"passed": False, "login_status": status}
    bearer = result["token"]
    status, sites = api(base, "GET", "/api/v1/sites", bearer=bearer)
    report = {"login_status": 200, "sites_status": status, "sites_count": len(sites) if isinstance(sites, list) else None}
    status, _ = api(base, "GET", "/api/v1/system/metrics", bearer=bearer)
    report["metrics_status"] = status
    report["passed"] = report["sites_status"] == 200 and status == 200
    status, backend = api(base, "GET", "/api/v1/scheduler/status", bearer=bearer)
    report["scheduler_status_code"] = status
    report["scheduler_status"] = backend
    if data_dir == Path("/var/lib/fluxo"):
        status, site = api(base, "POST", "/api/v1/sites", {"domain": "arm64-probe.invalid", "app_type": "laravel", "php_version": "8.4", "web_root": "public", "deployment_strategy": "standard", "install_composer": False}, bearer)
        report["php_site_creation_status"] = status
        if status not in [200, 201] or not isinstance(site, dict):
            report["php_site_error"] = site
            report["passed"] = False
            return report
        path = Path(site["path"])
        if not str(path).startswith("/home/fluxo/"):
            raise RuntimeError("New fixture site path is outside the managed site root.")
        account = pwd.getpwnam("fluxo")
        fixtures = {path / "artisan": "<?php file_put_contents(__DIR__.'/arm64-ticks.log', (string)posix_geteuid().chr(10), FILE_APPEND);\n", path / "public" / "arm64-probe.php": "<?php echo 'fluxo-arm64-php-ok';\n"}
        for target, content in fixtures.items():
            fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
            with os.fdopen(fd, "w") as stream:
                stream.write(content)
            os.chown(target, account.pw_uid, account.pw_gid)
        try:
            request = urllib.request.Request("http://127.0.0.1/arm64-probe.php", headers={"Host": "arm64-probe.invalid"})
            with urllib.request.urlopen(request, timeout=15) as response:
                report["php_http_passed"] = response.status == 200 and response.read() == b"fluxo-arm64-php-ok"
        except Exception as error:
            report["php_http_passed"] = False
            report["php_http_error"] = sanitized(str(error))
        status, error = api(base, "POST", "/api/v1/sites/" + str(site["id"]) + "/crons", {"name": "Native ARM PHP fixture", "expression": "* * * * *", "command": "php8.4 artisan schedule:run", "user": "fluxo"}, bearer)
        report["scheduler_registration_status"] = status
        report["scheduler_registration_error"] = error if status != 201 else None
        if status == 201:
            deadline = time.monotonic() + 100
            ticks = path / "arm64-ticks.log"
            while time.monotonic() < deadline and not ticks.exists():
                time.sleep(1)
            report["genuine_nonroot_php_tick"] = ticks.exists() and all(line == str(account.pw_uid) for line in ticks.read_text().splitlines())
        report["passed"] = report["passed"] and report.get("php_http_passed", False) and report.get("genuine_nonroot_php_tick", False)
    return report


def private_core_probe(binary, private):
    """Test core boot/SQLite/API even if system provisioning was refused."""
    data_dir = private / "core-data"
    data_dir.mkdir(mode=0o700)
    env = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/root", "LANG": "C.UTF-8", "FLUXO_ENV": "dev", "FLUXO_DATA_DIR": str(data_dir), "FLUXO_PORT": "19595"}
    log_path = private / "core.log"
    with log_path.open("x") as log:
        os.chmod(log_path, 0o600)
        child = subprocess.Popen([str(binary)], env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 45
            while child.poll() is None and time.monotonic() < deadline:
                try:
                    status, version = api("https://127.0.0.1:19595", "GET", "/api/v1/version")
                    if status == 200:
                        checks = basic_api_checks("https://127.0.0.1:19595", data_dir)
                        return {"version": version, "sqlite_created": (data_dir / "fluxo.db").is_file(), "authenticated_api": checks, "passed": checks["passed"], "scope": "development-mode native core only; not installed execution acceptance"}
                except (OSError, urllib.error.URLError):
                    time.sleep(.5)
            return {"passed": False, "exit_code": child.poll(), "startup_error": sanitized(log_path.read_text(errors="replace")[-5000:])}
        finally:
            if child.poll() is None:
                child.terminate()
                try:
                    child.wait(timeout=25)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait(timeout=5)


def main():
    required = {"FLUXO_ARM64_RELEASE_PROBE": "1", "GITHUB_ACTIONS": "true", "RUNNER_ARCH": "ARM64", "RUNNER_ENVIRONMENT": "github-hosted", "GITHUB_REF": "refs/heads/test/arm64-bridge-acceptance"}
    if os.geteuid() != 0 or platform.machine() != "aarch64" or any(os.environ.get(k) != v for k, v in required.items()):
        raise SystemExit("This probe requires the opted-in native ARM64 hosted runner on its isolated branch.")
    if run(["dpkg", "--print-architecture"])["stdout"] != "arm64":
        raise SystemExit("Expected native arm64 package architecture.")
    if Path("/usr/local/bin/fluxo").exists() or Path("/var/lib/fluxo/fluxo.db").exists():
        raise SystemExit("Refusing to reuse an existing Fluxo installation.")
    output = Path(os.environ["FLUXO_PROBE_OUTPUT"])
    output.mkdir(mode=0o755, parents=True, exist_ok=False)
    report = {"release": TAG, "published_binary_sha256": EXPECTED_BINARY, "published_installer_sha256": EXPECTED_INSTALLER, "native_arm64": True, "hosted_runner_is_not_pristine_vps": True, "source_changed": False, "local_binary_override": False, "attestation_bypass": False, "scope": "unchanged published base installation and read-only diagnostics", "native_production_approved": False, "application_workflow_acceptance_completed": False, "passed": False}
    timers = {u: run(["systemctl", "is-active", "--quiet", u])["exit_code"] == 0 for u in ["apt-daily.timer", "apt-daily-upgrade.timer"]}
    private = Path(tempfile.mkdtemp(prefix="fluxo-arm64-probe-", dir="/root"))
    os.chmod(private, 0o700)
    try:
        report["environment"] = {"uname": run(["uname", "-srmo"]), "os_release": Path("/usr/lib/os-release").read_text(), "packages_before": run(["dpkg-query", "-W", "-f=${Package} ${Version} ${Architecture}\n", "apt", "libc6", "cron", "cronie", "libpam0g", "libpam-modules", "libpam-systemd", "libcap2", "libselinux1"]), "systemd": run(["systemctl", "--version"])}
        binary = private / "fluxo-linux-arm64"
        installer = private / "install.sh"
        download("fluxo-linux-arm64", binary, EXPECTED_BINARY)
        download("install.sh", installer, EXPECTED_INSTALLER)
        binary.chmod(0o700)
        report["binary_metadata"] = {flag: run([str(binary), flag]) for flag in ["--version", "--execution-capabilities", "--bridge-contract"]}
        if any(r["exit_code"] != 0 for r in report["binary_metadata"].values()):
            raise RuntimeError("Unchanged ARM binary failed a pure metadata command.")
        if report["binary_metadata"]["--version"]["stdout"] != "fluxo version 0.4.47":
            raise RuntimeError("Published binary version differs.")
        report["disposable_runner_system_paths"] = runner_system_paths()
        for unit in timers:
            result = run(["systemctl", "stop", unit])
            if result["exit_code"] != 0:
                raise RuntimeError("Could not pause the runner's APT timer.")
        # The installer owns package admission; no lock or busy-work bypass here.
        args = ["bash", str(installer), "--db-engine=none", "--no-redis", "--no-node", "--no-python", "--no-harden-ssh"]
        report["installer_options"] = args[2:]
        env = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/root", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "DEBIAN_FRONTEND": "noninteractive", "FLUXO_VERSION": TAG}
        log_path = private / "installer.log"
        try:
            with log_path.open("x") as log:
                os.chmod(log_path, 0o600)
                installed = subprocess.run(args, env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, timeout=840)
            report["installer_exit_code"] = installed.returncode
        except subprocess.TimeoutExpired:
            report["installer_timed_out"] = True
            report["installer_exit_code"] = 124
        raw_log = log_path.read_text(errors="replace")
        (output / "installer-sanitized.log").write_text(sanitized(raw_log) + "\n")
        report["private_raw_log_sha256"] = digest(log_path)
        report["installer_provenance_verified"] = "Fluxo release provenance verified" in raw_log
        report["installer_provenance_skipped"] = "Skipping release attestation" in raw_log
        report["service"] = run(["systemctl", "show", "fluxo.service", "--property=ActiveState,SubState,MainPID"])
        installed_binary = Path("/usr/local/bin/fluxo")
        if installed_binary.exists():
            report["installed_binary_sha256"] = digest(installed_binary)
            prod = dict(env, FLUXO_ENV="prod", FLUXO_DATA_DIR="/var/lib/fluxo")
            report["readiness"] = run([str(installed_binary), "baseline-bridge", "inspect"], timeout=90, env=prod)
            report["scheduler_loader"] = run([str(installed_binary), "scheduler-loader", "inspect"], timeout=90, env=prod)
            try:
                with urllib.request.urlopen("https://127.0.0.1:9595/api/v1/version", context=ssl._create_unverified_context(), timeout=10) as response:
                    report["http_version"] = json.load(response)
                report["authenticated_api"] = basic_api_checks("https://127.0.0.1:9595", Path("/var/lib/fluxo"))
            except Exception as error:
                report["http_error"] = str(error)
        report["execution_state"] = {name: state_summary(Path(path)) for name, path in {"reader_floor": "/var/lib/fluxo/execution-readers.json", "installer_intent": "/var/lib/fluxo/execution-installer.json", "scheduler": "/var/lib/fluxo-scheduler/state.json", "system_tasks": "/var/lib/fluxo-system-tasks/state.json"}.items()}
        state = report["execution_state"]
        report["passed"] = report["installer_exit_code"] == 0 and report.get("installed_binary_sha256") == EXPECTED_BINARY and report["installer_provenance_verified"] and not report["installer_provenance_skipped"] and "http_version" in report and state["reader_floor"].get("phase") == "committed" and state["installer_intent"].get("phase") == "complete" and report.get("authenticated_api", {}).get("passed", False)
        if "http_version" not in report:
            report["private_core_probe"] = private_core_probe(binary, private)
    except Exception as error:
        report["probe_error"] = sanitized(str(error))
    finally:
        run(["systemctl", "stop", "fluxo.service"], timeout=60)
        for unit, active in timers.items():
            run(["systemctl", "start" if active else "stop", unit])
        report["apt_timers_restored"] = all((run(["systemctl", "is-active", "--quiet", u])["exit_code"] == 0) == active for u, active in timers.items())
        report["passed"] = report["passed"] and report["apt_timers_restored"]
        (output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        # No raw logs, database contents, keys or process environment are uploaded.
        print(json.dumps({"passed": report["passed"], "native_arm64": True, "installer_exit_code": report.get("installer_exit_code"), "probe_error": report.get("probe_error"), "report": str(output / "report.json")}))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
