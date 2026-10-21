"""Resolver contract fixtures: no root access or package mutation."""
import runpy
import hashlib
import pathlib
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

bridge = runpy.run_path(sys.argv[1], run_name="fixture")
apt_pkg = bridge["apt_pkg"]


class Version:
    def __init__(self, version, source="php8.4", origin="Ubuntu"):
        self.version = version
        self.architecture = apt_pkg.config.find("APT::Architecture")
        self.source_name = source
        self.source_version = version
        self.size = 100
        self.record = {"SHA256": "a" * 64}
        self.origins = [types.SimpleNamespace(origin=origin, label=origin, archive="noble-security", component="main", site="security.ubuntu.com", trusted=True)]


class Package:
    def __init__(self, name, old="8.4.1-0ubuntu1", new="8.4.1-0ubuntu2", source="php8.4"):
        self.shortname = name
        self.installed = Version(old, source)
        self.candidate = Version(new, source)
        self.is_installed = True
        self.is_auto_installed = True
        self._pkg = types.SimpleNamespace(selected_state=apt_pkg.SELSTATE_INSTALL, current_state=apt_pkg.CURSTATE_INSTALLED)
        self.marked_install = self.marked_delete = self.marked_upgrade = False

    def mark_install(self, auto_fix, auto_inst, from_user):
        assert not auto_fix and not auto_inst and not from_user
        self.marked_install = self.marked_upgrade = True


class Cache(list):
    broken_count = 0

    def clear(self):
        for p in self:
            p.marked_install = p.marked_upgrade = False

    def __getitem__(self, name):
        return next(p for p in self if p.shortname == name.split(":")[0])

    def get_changes(self):
        return [p for p in self if p.marked_upgrade]


class Planner(unittest.TestCase):
    def setUp(self):
        self.cache = Cache(Package("php8.4-" + n) for n in ["fpm", "cli", "common", "opcache"])

    def plan(self):
        return bridge["inspect"](self.cache, "8.4")

    def test_backport_revision_and_automatic_marks(self):
        plan = self.plan()
        self.assertEqual([], plan["blocked_reasons"])
        self.assertEqual(4, len(plan["changes"]))
        self.assertTrue(all(p["automatic"] for p in plan["packages"]))
        self.assertGreater(apt_pkg.version_compare("1:8.4.1-1", "8.4.99-9"), 0)

    def test_independent_extension_version(self):
        self.cache.append(Package("php8.4-redis", "6.1.0-1ubuntu1", "6.1.0-1ubuntu2", "php-redis"))
        self.assertEqual([], self.plan()["blocked_reasons"])
        self.assertEqual(5, len(self.plan()["changes"]))

    def test_held_core_blocks(self):
        self.cache["php8.4-common"]._pkg.selected_state = apt_pkg.SELSTATE_HOLD
        self.assertIn("php8.4-common is held", self.plan()["blocked_reasons"])

    def test_new_dependency_blocks(self):
        self.cache.broken_count = 1
        self.assertTrue(self.plan()["blocked_reasons"])

    def test_installed_only_candidate_is_not_current(self):
        p = self.cache["php8.4-cli"]
        p.candidate.version = p.installed.version
        p.candidate.origins = []
        self.assertIn("No usable trusted repository candidate for php8.4-cli", self.plan()["blocked_reasons"])

    def test_inconsistent_source_candidates_block(self):
        self.cache["php8.4-cli"].candidate.source_version = "8.4.1-0ubuntu3"
        self.assertTrue(self.plan()["blocked_reasons"])

    def test_downgrade_and_untrusted_origins_block(self):
        self.cache["php8.4-cli"].candidate.version = "8.3.0-1"
        self.cache["php8.4-fpm"].candidate.origins[0].trusted = False
        self.assertGreaterEqual(len(self.plan()["blocked_reasons"]), 2)

    def test_resolver_extra_change_blocks(self):
        extra = Package("unrelated-service")
        extra.marked_upgrade = True
        self.cache.get_changes = lambda: [p for p in self.cache if p.marked_upgrade] + [extra]
        self.assertTrue(self.plan()["blocked_reasons"])


class NativePayload(unittest.TestCase):
    """Inspect actual .deb payloads without installing or modifying packages."""

    def test_equal_revision_different_library_is_rejected(self):
        approved = b"approved native runtime bytes"
        binding = dict(package="php8.4-swoole", architecture="amd64",
                       member="usr/lib/php/20240924/swoole.so",
                       sha256=hashlib.sha256(approved).hexdigest(), size=len(approved))
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            for number, payload in enumerate([approved, b"replaced native runtime bytes"]):
                package = root / str(number)
                control = package / "DEBIAN"
                control.mkdir(parents=True)
                (control / "control").write_text("Package: php8.4-swoole\nVersion: 6.2.3-1\nArchitecture: amd64\nMaintainer: Fluxo acceptance <test@example.invalid>\nDescription: Native archive verification fixture\n")
                library = package / binding["member"]
                library.parent.mkdir(parents=True)
                library.write_bytes(payload)
                archive = root / (str(number) + ".deb")
                subprocess.run(["dpkg-deb", "--build", "--root-owner-group", str(package), str(archive)], check=True, stdout=subprocess.DEVNULL)
                sha = hashlib.sha256(archive.read_bytes()).hexdigest()
                archive.rename(root / (sha + ".deb"))
                target = dict(name=binding["package"], architecture="amd64", version="6.2.3-1", sha256=sha)
                if number == 0:
                    bridge["verify_native_payloads"](directory, [target], [binding])
                else:
                    with self.assertRaisesRegex(RuntimeError, "differs from the approved"):
                        bridge["verify_native_payloads"](directory, [target], [binding])

    def test_missing_native_package_is_rejected(self):
        binding = dict(package="php8.4-swoole", architecture="amd64",
                       member="usr/lib/php/20240924/swoole.so", sha256="a" * 64, size=10)
        with self.assertRaisesRegex(RuntimeError, "missing from the retained"):
            bridge["verify_native_payloads"]("/unused", [], [binding])


class LockErrors(unittest.TestCase):
    def test_interrupted_journal_does_not_masquerade_as_busy(self):
        with patch.object(apt_pkg, "pkgsystem_lock", side_effect=SystemError("E:dpkg was interrupted")), patch.object(bridge["time"], "sleep") as sleep:
            with self.assertRaisesRegex(RuntimeError, "journal requires operator repair"):
                with bridge["system_lock"]():
                    self.fail("an interrupted journal was admitted")
            sleep.assert_not_called()


unittest.main(argv=["php-update-planner"])
