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
import re
import ssl
import subprocess
import tempfile
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
            except Exception as error:
                report["http_error"] = str(error)
        report["execution_state"] = {name: state_summary(Path(path)) for name, path in {"reader_floor": "/var/lib/fluxo/execution-readers.json", "installer_intent": "/var/lib/fluxo/execution-installer.json", "scheduler": "/var/lib/fluxo-scheduler/state.json", "system_tasks": "/var/lib/fluxo-system-tasks/state.json"}.items()}
        state = report["execution_state"]
        report["passed"] = report["installer_exit_code"] == 0 and report.get("installed_binary_sha256") == EXPECTED_BINARY and report["installer_provenance_verified"] and not report["installer_provenance_skipped"] and "http_version" in report and state["reader_floor"].get("phase") == "committed" and state["installer_intent"].get("phase") == "complete"
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
