"""No-package live CLI acceptance, inside a private /var/lib mount.

Usage (root on the reusable VM): unshare --mount --propagation private ...
Set FLUXO_PHP_UPDATE_VM_TEST=1 and FLUXO_SCHEDULER_ISOLATED_MOUNTS=1.
Arguments: cmd/fluxo test executable, scratch root outside /var/lib.
"""
import concurrent.futures
import hashlib
import json
import os
import pathlib
import shutil
import socket
import sqlite3
import subprocess
import sys
import time
import unittest
import urllib.request


@unittest.skipUnless(os.geteuid() == 0 and socket.gethostname() == "fluxo-php-patch-test"
                     and os.environ.get("FLUXO_PHP_UPDATE_VM_TEST") == "1"
                     and os.environ.get("FLUXO_SCHEDULER_ISOLATED_MOUNTS") == "1",
                     "guarded isolated reusable VM only")
class PanelVerification(unittest.TestCase):
    def test_live_verification_drains_and_preserves_safety_boundaries(self):
        executable, root = map(pathlib.Path, sys.argv[1:3])
        binary = pathlib.Path("/usr/local/bin/fluxo")
        unit = pathlib.Path("/etc/systemd/system/fluxo.service")
        self.assertFalse(binary.exists(), "preserve an existing panel binary")
        self.assertFalse(unit.exists(), "preserve an existing panel unit")
        self.assertNotEqual(subprocess.run(["systemctl", "is-active", "--quiet", "fluxo"]).returncode, 0)
        app = pathlib.Path("/home/fluxo") / root.name
        self.assertFalse(app.exists())
        app.mkdir(mode=0o755)
        data = root / "data"
        data.mkdir()
        database = data / "fluxo.db"
        db = sqlite3.connect(database)
        db.execute("CREATE TABLE php_update_generations(id TEXT, state TEXT)")
        db.commit()
        journal = pathlib.Path("/var/lib/fluxo-scheduler/state.json")
        support_file = root / "support"
        env = {**os.environ, "FLUXO_PANEL_VERIFY_FIXTURE": "cli", "FLUXO_DATA_DIR": str(data),
               "FLUXO_PANEL_VERIFY_SUPPORT_FILE": str(support_file)}

        def run(*args, **kwargs):
            return subprocess.run(args, text=True, capture_output=True, timeout=25, **kwargs)

        def verify():
            return run(str(executable), "scheduler-verify-panel", env=env)

        def start(mode="panel"):
            support_file.write_text("0" if mode == "old" else "3")
            unit.write_text("[Service]\nType=simple\nUser=root\nExecStart=/usr/local/bin/fluxo\n"
                            "PrivateTmp=yes\nKillMode=control-group\nDelegate=no\n"
                            "Environment=FLUXO_PHP_UPDATE_VM_TEST=1\n"
                            f"Environment=FLUXO_PANEL_VERIFY_FIXTURE={mode}\n"
                            f"Environment=FLUXO_PANEL_VERIFY_APP={app}\n")
            self.assertEqual(run("systemctl", "daemon-reload").returncode, 0)
            self.assertEqual(run("systemctl", "start", "fluxo").returncode, 0)
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline:
                try:
                    with urllib.request.urlopen("http://127.0.0.1:19795/health", timeout=1):
                        return
                except OSError:
                    time.sleep(.1)
            self.fail("fixture panel did not start")

        def request():
            req = urllib.request.Request("http://127.0.0.1:19795/run", method="POST")
            with urllib.request.urlopen(req, timeout=40) as response:
                return response.read().decode()

        try:
            shutil.copyfile(executable, binary)
            binary.chmod(0o755)
            # First reproduce an upgrade from a panel without supported admission.
            start("old")
            rejected = verify()
            self.assertNotEqual(rejected.returncode, 0)
            self.assertIn("supported Scheduler admission", rejected.stderr)
            self.assertFalse(journal.exists(), "unsupported panel created fabricated proof")
            self.assertEqual(run("systemctl", "stop", "fluxo").returncode, 0)
            start()
            pid = run("systemctl", "show", "fluxo", "-p", "MainPID", "--value").stdout.strip()
            # Retain output pipes and PrivateTmp through a session-detached child.
            (app / "artisan").write_text('''<?php
file_put_contents(__DIR__.'/started','yes');file_put_contents('/tmp/fluxo-panel-verification','alive');
echo "output-before-drain\\n";
$p=pcntl_fork();if($p>0)exit(0);posix_setsid();
usleep(3000000);
if(!is_file('/tmp/fluxo-panel-verification'))exit(3);
file_put_contents(__DIR__.'/completed','yes');echo "output-after-drain\\n";
''')
            self.assertEqual(run("chown", "-R", "fluxo:fluxo", str(app)).returncode, 0)
            with concurrent.futures.ThreadPoolExecutor() as pool:
                work = pool.submit(request)
                deadline = time.monotonic() + 5
                while not (app / "started").exists() and time.monotonic() < deadline:
                    time.sleep(.05)
                self.assertTrue((app / "started").exists())
                before = time.monotonic()
                accepted = verify()
                self.assertEqual(accepted.returncode, 0, accepted.stderr)
                self.assertGreater(time.monotonic() - before, 2)
                self.assertIn("output-after-drain", work.result())
            self.assertTrue((app / "completed").exists())
            self.assertEqual(run("systemctl", "show", "fluxo", "-p", "MainPID", "--value").stdout.strip(), pid)
            original = json.loads(journal.read_text())
            self.assertIsNone(original["upgrade"])
            self.assertEqual(original["panel_proof"]["binary"], hashlib.sha256(executable.read_bytes()).hexdigest())
            # A verification timeout must not cancel the other request's job.
            (app / "started").unlink()
            (app / "completed").unlink()
            (app / "artisan").write_text('''<?php
file_put_contents(__DIR__.'/started','yes');usleep(23000000);
file_put_contents(__DIR__.'/completed','yes');echo "completed-after-timeout\\n";
''')
            before = journal.read_bytes()
            with concurrent.futures.ThreadPoolExecutor() as pool:
                work = pool.submit(request)
                deadline = time.monotonic() + 5
                while not (app / "started").exists() and time.monotonic() < deadline:
                    time.sleep(.05)
                self.assertTrue((app / "started").exists())
                rejected = verify()
                self.assertNotEqual(rejected.returncode, 0)
                self.assertIn("maintenance lock", rejected.stderr)
                self.assertFalse((app / "completed").exists())
                self.assertEqual(before, journal.read_bytes())
                self.assertIn("completed-after-timeout", work.result())
            self.assertEqual(run("systemctl", "show", "fluxo", "-p", "MainPID", "--value").stdout.strip(), pid)
            # Add unresolved cron history: verification must not certify/erase it.
            boot = pathlib.Path("/proc/sys/kernel/random/boot_id").read_text().strip()
            original["legacy"]["8.4"] = boot
            original["panel_proof"] = None
            journal.write_text(json.dumps(original))
            self.assertEqual(verify().returncode, 0)
            state = json.loads(journal.read_text())
            self.assertEqual(state["legacy"], original["legacy"])
            self.assertEqual(state["migrations"], original["migrations"])
            mismatched = root / "different-panel"
            mismatched.write_bytes(executable.read_bytes() + b"\n")
            mismatched.chmod(0o755)
            before = journal.read_bytes()
            rejected = run(str(mismatched), "scheduler-verify-panel", env=env)
            self.assertNotEqual(rejected.returncode, 0, "a different running panel was certified")
            self.assertEqual(before, journal.read_bytes())
            self.assertEqual(verify().returncode, 0)
            state = json.loads(journal.read_text())
            db.execute("INSERT INTO php_update_generations VALUES('pending','recovery_pending')")
            db.commit()
            before = journal.read_bytes()
            rejected = verify()
            self.assertNotEqual(rejected.returncode, 0)
            self.assertIn("maintenance", rejected.stderr)
            self.assertEqual(before, journal.read_bytes())
            db.execute("DELETE FROM php_update_generations")
            db.commit()
            # A durable installer marker is not silently stolen by this repair.
            state["upgrade"] = {"id": "87d6b4a0-e0dd-4c57-a9c9-a5ae175c99dc", "boot": boot, "pid": 999999, "start": "1"}
            journal.write_text(json.dumps(state))
            before = journal.read_bytes()
            rejected = verify()
            self.assertNotEqual(rejected.returncode, 0)
            self.assertIn("unfinished installer", rejected.stderr)
            self.assertEqual(before, journal.read_bytes())
            state["upgrade"] = None
            state["holds"]["8.4"] = "62b9629d-9f50-4db2-b4b7-608f63d85ae7"
            journal.write_text(json.dumps(state))
            before = journal.read_bytes()
            self.assertNotEqual(verify().returncode, 0)
            self.assertEqual(before, journal.read_bytes())
            print("Verified no-restart drain, unchanged panel PID, legacy history/holds/installer preservation")
        finally:
            db.close()
            run("systemctl", "stop", "fluxo")
            unit.unlink(missing_ok=True)
            binary.unlink(missing_ok=True)
            run("systemctl", "daemon-reload")
            shutil.rmtree(app)


if __name__ == "__main__":
    unittest.main(argv=[sys.argv[0]], verbosity=2)
