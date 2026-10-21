"""Installer lease fixtures. Host checks use only temporary mock commands.
Real-helper checks require the explicitly guarded reusable Ubuntu VM.
"""
import contextlib
import fcntl
import json
import os
import pathlib
import select
import shutil
import signal
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
import uuid


class InstallerCleanup(unittest.TestCase):
    def test_readiness_timeout_and_release_failure_are_bounded(self):
        source = pathlib.Path(sys.argv[1]).read_text()
        start = source.index("check_scheduler_candidate_compatibility() {")
        end = source.index("\nprepare_upgrade_snapshot()", start)
        functions = source[start:end].replace("/usr/local/bin/.fluxo-installer-lease.XXXXXX", "$FIXTURE_DIR/.fluxo-installer-lease.XXXXXX")
        with tempfile.TemporaryDirectory(prefix="fluxo-installer-lease-") as temporary:
            root = pathlib.Path(temporary)
            helper = root / "helper"
            helper.write_text('''#!/bin/bash
set -eu
case "$1" in
 --scheduler-support) echo 3 ;;
 scheduler-installer-lease)
  sleep "$LEASE_DELAY"
  trap 'exit 0' USR1
  echo "$$" > "$FIXTURE_DIR/helper.pid"
  echo 'READY 12345678-1234-4234-8234-123456789abc'
  while kill -0 "$2" 2>/dev/null; do sleep 0.1; done
  ;;
 scheduler-installer-release)
  if [ "$LEASE_RELEASE" = success ]; then kill -USR1 "$(cat "$FIXTURE_DIR/helper.pid")"; else exit 1; fi ;;
 --scheduler-compatibility-required) echo "${LEASE_REQUIRE_COMPAT:-0}" ;;
esac
''')
            helper.chmod(0o700)
            script = root / "test.sh"
            script.write_text('''#!/bin/bash
set -u
sudo() { if [ "$1" = install ]; then command install -m 0755 "${@: -2}"; else "$@"; fi; }
SCHEDULER_LEASE_DIR=""
SCHEDULER_LEASE_PID=""
SCHEDULER_LEASE_ID=""
SCHEDULER_LEASE_BINARY=""
CANDIDATE_BINARY="$1"
''' + functions + '''
begin_scheduler_installer_lease; begin_status=$?
if [ "$begin_status" = 0 ]; then mv "$CANDIDATE_BINARY" "$FIXTURE_DIR/published"; CANDIDATE_BINARY=""; fi
end_scheduler_installer_lease; end_status=$?
printf 'RESULT %s %s\n' "$begin_status" "$end_status"
''')
            for delay, release_mode, limit, expected in [(13,"failure",21,"RESULT 1 1"),(0,"failure",9,"RESULT 0 1"),(0,"success",5,"RESULT 0 0")]:
                candidate=root / "candidate"
                shutil.copyfile(helper,candidate)
                candidate.chmod(0o700)
                before = time.monotonic()
                result = subprocess.run(["/bin/bash", str(script), str(candidate)], env={**os.environ, "LEASE_DELAY": str(delay),"FIXTURE_DIR":str(root),"LEASE_RELEASE":release_mode}, text=True, capture_output=True, timeout=limit)
                self.assertIn(expected, result.stdout, result.stdout + result.stderr)
                self.assertLess(time.monotonic() - before, limit)

    def test_rollback_guard_uses_retained_binary_after_publication(self):
        source=pathlib.Path(sys.argv[1]).read_text()
        start=source.index("rollback_upgrade() {")
        end=source.index('    echo "ERROR: Upgrade failed. Restoring the previous Fluxo release snapshot..."',start)
        function=source[start:end]+"    echo ROLLBACK_ALLOWED\n}\n"
        with tempfile.TemporaryDirectory(prefix="fluxo-rollback-guard-") as temporary:
            root=pathlib.Path(temporary)
            current=root/"retained-helper"
            current.write_text('#!/bin/bash\ncase "$1" in --scheduler-compatibility-required) echo "$REQUIRE_COMPAT";; --scheduler-required-support) echo "${REQUIRED_SUPPORT:-2}";; esac\n')
            current.chmod(0o700)
            backup=root/"backup";backup.mkdir()
            previous=backup/"fluxo"
            previous.write_text('#!/bin/bash\n[ "$1" = --scheduler-support ] && echo "$PREVIOUS_SUPPORT"\n')
            previous.chmod(0o700)
            script=root/"guard.sh"
            script.write_text('sudo() { if [ "$1" = systemctl ]; then return 0; else "$@"; fi; }\nCANDIDATE_BINARY=""\nSCHEDULER_LEASE_BINARY="$1"\nUPGRADE_BACKUP_DIR="$2"\n'+function+'rollback_upgrade\n')
            for required,support,expected in [("0","0",0),("1","2",0),("1","0",1)]:
                result=subprocess.run(["/bin/bash",str(script),str(current),str(backup)],env={**os.environ,"REQUIRE_COMPAT":required,"PREVIOUS_SUPPORT":support},text=True,capture_output=True,timeout=5)
                self.assertEqual(expected,result.returncode,result.stdout+result.stderr)
                if expected==0:self.assertIn("ROLLBACK_ALLOWED",result.stdout)
                else:self.assertIn("incompatible panel binary",result.stdout)


class InstallerBackendCompatibility(unittest.TestCase):
    def test_early_and_post_ready_compatibility_and_cleanup(self):
        source = pathlib.Path(sys.argv[1]).read_text()
        functions = source[source.index("check_scheduler_candidate_compatibility() {"):source.index("\nprepare_upgrade_snapshot()")]
        scenarios = [
            ("intent", ["backend-proofs.initializing"], "none", "2", False),
            ("intent-proof", ["backend-proofs.initializing", "backend-proofs.json"], "none", "2", False),
            ("lost-anchor", ["backend-proofs.json"], "none", "2", False),
            ("initialized", ["backend-proofs.json", "backend-proofs.initialized"], "none", "2", False),
            ("racing-intent", [], "intent", "2", False),
            ("racing-initialized", [], "initialized", "2", False),
            ("compatible-intent", ["backend-proofs.initializing"], "none", "3", True),
            ("compatible-race", [], "initialized", "3", True),
            ("evidence-free-old", [], "none", "2", True),
            ("inspection-error", [], "error", "2", False),
        ]
        for name, files, create, support, allowed in scenarios:
            with self.subTest(name=name), tempfile.TemporaryDirectory(prefix="fluxo-backend-compat-") as temporary:
                root = pathlib.Path(temporary)
                state = root / "state"
                if create == "error":
                    state.write_text("not a directory")
                else:
                    state.mkdir()
                    for file in files:
                        (state / file).write_text("retained evidence")
                helper = root / "candidate"
                helper.write_text('''#!/bin/bash
set -eu
case "$1" in
 --scheduler-support) echo "$CANDIDATE_SUPPORT" ;;
 scheduler-installer-lease)
  echo "$$" > "$FIXTURE_DIR/helper.pid"
  touch "$FIXTURE_DIR/lease-started"
  trap 'touch "$FIXTURE_DIR/released"; exit 0' USR1
  if [ "$LEASE_CREATE" = intent ]; then touch "$FIXTURE_DIR/state/backend-proofs.initializing"; fi
  if [ "$LEASE_CREATE" = initialized ]; then touch "$FIXTURE_DIR/state/backend-proofs.json" "$FIXTURE_DIR/state/backend-proofs.initialized"; fi
  echo 'READY 12345678-1234-4234-8234-123456789abc'
  while kill -0 "$2" 2>/dev/null; do sleep 0.05; done ;;
 scheduler-installer-release) kill -USR1 "$(cat "$FIXTURE_DIR/helper.pid")" ;;
esac
''')
                helper.chmod(0o700)
                isolated = functions.replace("/var/lib/fluxo-scheduler/", str(state) + "/").replace("/usr/local/bin/.fluxo-installer-lease.XXXXXX", "$FIXTURE_DIR/.fluxo-installer-lease.XXXXXX")
                script = root / "fixture.sh"
                script.write_text('''#!/bin/bash
set -u
sudo() { if [ "$1" = install ]; then command install -m 0755 "${@: -2}"; else "$@"; fi; }
SCHEDULER_LEASE_DIR=""
SCHEDULER_LEASE_PID=""
SCHEDULER_LEASE_ID=""
SCHEDULER_LEASE_BINARY=""
CANDIDATE_BINARY="$FIXTURE_DIR/candidate"
''' + isolated + '''
begin_scheduler_installer_lease; result=$?
if [ "$result" = 0 ]; then touch "$FIXTURE_DIR/panel-stopped"; fi
end_scheduler_installer_lease; cleanup=$?
printf 'RESULT %s %s\\n' "$result" "$cleanup"
''')
                result = subprocess.run(["/bin/bash", str(script)], env={**os.environ, "FIXTURE_DIR": str(root), "CANDIDATE_SUPPORT": support, "LEASE_CREATE": create}, text=True, capture_output=True, timeout=8)
                self.assertIn("RESULT " + ("0" if allowed else "1") + " 0", result.stdout, result.stdout + result.stderr)
                self.assertEqual(allowed, (root / "panel-stopped").exists())
                if (root / "lease-started").exists():
                    self.assertTrue((root / "released").exists(), "acquired lease was not released")
                for file in files:
                    self.assertEqual("retained evidence", (state / file).read_text())


@unittest.skipUnless(os.environ.get("FLUXO_PHP_UPDATE_VM_TEST") == "1" and os.geteuid() == 0 and socket.gethostname() == "fluxo-php-patch-test", "guarded reusable VM only")
class RealInstallerLease(unittest.TestCase):
    def test_success_crash_stale_retry_and_initialization_timeout(self):
        binary = pathlib.Path(sys.argv[2])
        self.assertTrue(binary.is_file())
        root = pathlib.Path(tempfile.mkdtemp(prefix="fluxo-installer-lease-acceptance-", dir="/var/lib"))
        database = root / "fluxo.db"
        connection = sqlite3.connect(database)
        connection.execute("CREATE TABLE php_update_generations(id TEXT PRIMARY KEY, state TEXT)")
        connection.commit()
        connection.close()
        environment = {**os.environ, "FLUXO_ENV": "prod", "FLUXO_DATA_DIR": str(root)}
        journal = pathlib.Path("/var/lib/fluxo-scheduler/state.json")
        children = []

        def start():
            child = subprocess.Popen([str(binary), "scheduler-installer-lease", str(os.getpid())], env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            children.append(child)
            return child

        def ready(child):
            readable, _, _ = select.select([child.stdout], [], [], 15)
            self.assertTrue(readable, "lease readiness timed out")
            line = child.stdout.readline().strip()
            self.assertTrue(line.startswith("READY "), line)
            return line.split()[1]

        def release(child, identity):
            subprocess.run([str(binary), "scheduler-installer-release", identity], check=True, text=True, capture_output=True, timeout=5)
            self.assertEqual(0, child.wait(timeout=5), child.stderr.read())
            self.assertIsNone(json.loads(journal.read_text())["upgrade"])

        try:
            child = start()
            identity = ready(child)
            self.assertEqual(identity, json.loads(journal.read_text())["upgrade"]["id"])
            locked = subprocess.run(["/usr/bin/flock", "-n", "/run/lock/fluxo-php-lifecycle.lock", "/usr/bin/true"])
            self.assertNotEqual(0, locked.returncode)
            wrong = subprocess.run([str(binary), "scheduler-installer-release", str(uuid.uuid4())], capture_output=True)
            self.assertNotEqual(0, wrong.returncode)
            release(child, identity)

            child = start()
            ready(child)
            child.kill()
            child.wait(timeout=5)
            self.assertIsNotNone(json.loads(journal.read_text())["upgrade"])
            retry = start()
            identity = ready(retry)
            release(retry, identity)

            # An active generation is rejected before an installer marker appears.
            connection = sqlite3.connect(database)
            connection.execute("INSERT INTO php_update_generations VALUES('pending','recovery_pending')")
            connection.commit()
            refused = start()
            self.assertNotEqual(0, refused.wait(timeout=5))
            self.assertIsNone(json.loads(journal.read_text())["upgrade"])
            connection.execute("DELETE FROM php_update_generations")
            connection.commit()
            connection.close()

            # The initialization flock is cancellable, including on established
            # state. It cannot make readiness or parent cleanup wait forever.
            with open("/var/lib/fluxo-scheduler.init.lock", "r+") as guard:
                fcntl.flock(guard, fcntl.LOCK_EX)
                delayed = start()
                self.assertNotEqual(0, delayed.wait(timeout=13))
                fcntl.flock(guard, fcntl.LOCK_UN)
            retry = start()
            identity = ready(retry)
            release(retry, identity)
            print("Preserved installer lease evidence:", root)
        finally:
            for child in children:
                if child.poll() is None:
                    child.kill()
                    child.wait(timeout=5)
                child.stdout.close()
                child.stderr.close()


unittest.main(argv=["installer-scheduler"])
