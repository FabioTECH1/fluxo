"""Signed package revisions for the isolated archive-retention acceptance test."""
import datetime
import email.utils
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import tempfile
import uuid

if (os.geteuid() != 0 or socket.gethostname() != "fluxo-php-patch-test"
        or os.environ.get("FLUXO_PHP_UPDATE_VM_TEST") != "1"):
    raise SystemExit("Explicit root disposable VM opt-in required")

mode, directory, generation = sys.argv[1:]
directory = Path(directory)
if directory.parent != Path("/var/lib/fluxo-php-recovery-acceptance-runs") or str(uuid.UUID(generation)) != generation:
    raise SystemExit("Unsupported isolated retention fixture")
repository = Path("/var/lib/fluxo-php-recovery-fixture-repo")
backup = directory / "retention-repository-before"


def run(*args):
    return subprocess.check_output(args, stderr=subprocess.STDOUT, timeout=180)


if mode == "restore":
    for name in ("Packages", "Release", "InRelease"):
        (repository / name).write_bytes((backup / name).read_bytes())
        (repository / name).chmod(0o644)
    run("apt-get", "-o", "APT::Update::Error-Mode=any", "update")
    print("Restored the existing fixture repository indexes; generated packages preserved.")
    raise SystemExit(0)
if mode != "prepare":
    raise SystemExit("Unknown retention fixture mode")

manifest = json.loads((directory / "php-updates" / generation / "archives" / "manifest.json").read_text())
sources = {p["name"]: p["source"] for p in manifest["packages"]}
backup.mkdir(mode=0o700)
for name in ("Packages", "Release", "InRelease"):
    (backup / name).write_bytes((repository / name).read_bytes())
    (backup / name).chmod(0o600)
published = repository / ("retention-" + generation)
published.mkdir(mode=0o755)
records = []
for package in manifest["after"]:
    if sources[package["name"]] != "php8.4":
        continue
    source = directory / "php-updates" / generation / "archives" / (package["sha256"] + ".deb")
    if hashlib.sha256(source.read_bytes()).hexdigest() != package["sha256"]:
        raise RuntimeError("Original authenticated fixture archive changed")
    version = re.sub(r"-.*$", "-999+ubuntu24.04.1+deb.sury.org+1", package["version"])
    with tempfile.TemporaryDirectory(prefix="retention-package-", dir=directory) as scratch:
        root = Path(scratch) / "package"
        run("dpkg-deb", "-R", str(source), str(root))
        control = root / "DEBIAN/control"
        text = control.read_text().replace(package["version"], version)
        control.write_text(text)
        target = published / (package["name"] + ".deb")
        run("dpkg-deb", "--build", "--root-owner-group", str(root), str(target))
    target.chmod(0o644)
    content = target.read_bytes()
    records.append(run("dpkg-deb", "-f", str(target)).decode().rstrip()
                   + "\nFilename: ./" + str(target.relative_to(repository))
                   + "\nSize: " + str(len(content))
                   + "\nSHA256: " + hashlib.sha256(content).hexdigest() + "\n")
if len(records) < 3:
    raise RuntimeError("Incomplete authenticated PHP source-package fixture")
packages = (backup / "Packages").read_bytes().rstrip() + b"\n\n" + ("\n".join(records) + "\n").encode()
(repository / "Packages").write_bytes(packages)
date = email.utils.format_datetime(datetime.datetime.now(datetime.timezone.utc), usegmt=True)
release = ("Origin: LP-PPA-ondrej-php\nLabel: Fluxo disposable PHP recovery fixture\n"
           "Suite: fluxo-test\nCodename: fluxo-test\nDate: " + date
           + "\nArchitectures: amd64\nSHA256:\n " + hashlib.sha256(packages).hexdigest()
           + " " + str(len(packages)) + " Packages\n")
(repository / "Release").write_text(release)
run("gpg", "--homedir", "/var/lib/fluxo-php-recovery-fixture-key", "--batch", "--yes",
    "--digest-algo", "SHA256", "--clearsign", "--output", str(repository / "InRelease"),
    str(repository / "Release"))
print(json.dumps({"authenticated_test_packages": len(records), "revision": version,
                  "repository": str(repository), "original_indexes": str(backup)}))
