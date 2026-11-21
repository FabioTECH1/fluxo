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

    def test_missing_live_key_does_not_authorize_but_allows_catalog_for_staged_repair(self):
        import json
        import os
        import stat
        load = bridge["load_retained_catalog"]
        original_open = open
        def read(path, *a, **k):
            if path == "/usr/share/keyrings/fluxo-sury-archive.gpg":
                raise FileNotFoundError(path)
            return original_open(path, *a, **k)
        with tempfile.TemporaryDirectory() as directory:
            retained = pathlib.Path(directory) / "retained"
            retained.mkdir()
            (pathlib.Path(directory)/"authorization.json").write_text(json.dumps(dict(policy_hash="old",key_sha256="pinned")))
            metadata=types.SimpleNamespace(st_mode=stat.S_IFREG|0o600,st_uid=0,st_size=100)
            with patch.dict(load.__globals__,open=read,private_directory=lambda p:None,REPOSITORY_AUTHORIZED=True),patch.object(os,"lstat",return_value=metadata):
                load(str(retained))
                self.assertFalse(load.__globals__["REPOSITORY_AUTHORIZED"])
                self.assertEqual(str(retained),load.__globals__["RETAINED_DIRECTORY"])

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

    def vendor_cache(self, installed="8.4.26-1+ubuntu24.04.1+deb.sury.org+1",
                     candidate="8.4.27-1+0~20261021.57+ubuntu24.04~1.gbp7636b9", suite="noble"):
        self.cache = Cache(Package("php8.4-" + name, installed, candidate) for name in ["fpm", "cli", "common", "opcache"])
        for pkg in self.cache:
            pkg.candidate.origins = [types.SimpleNamespace(origin="deb.sury.org", label="", archive=suite, component="main", site="packages.sury.org", trusted=True)]
            for version in [pkg.installed, pkg.candidate]:
                version.record.update(Package=pkg.shortname, Version=version.version, Source=version.source_name)

    def test_reported_markerless_candidate_and_subsequent_updates(self):
        for installed, candidate in [
                ("8.4.27-1+ubuntu24.04.1+deb.sury.org+1", "8.4.28-3+debsuryorg24.04.1+1"),
                ("8.4.28-3+debsuryorg24.04.1+1", "8.4.28-4+debsuryorg24.04.1+1")]:
            with self.subTest(installed=installed, candidate=candidate):
                self.vendor_cache(installed, candidate)
                with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
                    plan = self.plan()
                self.assertEqual([], plan["blocked_reasons"])
                self.assertEqual(4, len(plan["changes"]))

    def test_signed_packaging_labels_are_not_a_candidate_allowlist(self):
        for release, suite in [("22.04", "jammy"), ("24.04", "noble"), ("26.04", "resolute")]:
            for suffix in ["packaging2+3", "php8.4+1", "vendor3.2~build1+7"]:
                with self.subTest(release=release, suffix=suffix):
                    installed = "1:8.4.27-1+ubuntu" + release + ".1+deb.sury.org+1"
                    candidate = "1:8.4.28-4+debsuryorg" + release + ".1+" + suffix
                    self.assertFalse(bridge["sury_revision"](candidate))
                    self.vendor_cache(installed, candidate, suite)
                    with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
                        self.assertEqual([], self.plan()["blocked_reasons"])

    def test_signed_installed_record_handles_later_packaging_update(self):
        installed = "8.4.28-4+debsuryorg24.04.1+packaging2+3"
        candidate = "8.4.28-5+debsuryorg24.04.1+packaging3+1"
        self.vendor_cache(installed, candidate)
        # A version in dpkg status alone must not acquire repository provenance.
        with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
            self.assertTrue(any("continuity" in r for r in self.plan()["blocked_reasons"]))
            for pkg in self.cache:
                pkg.installed.origins = list(pkg.candidate.origins)
            self.assertEqual([], self.plan()["blocked_reasons"])

    def test_signed_packaging_fallback_retains_metadata_and_origin_guards(self):
        candidate = "8.4.28-4+debsuryorg24.04.1+packaging2+3"
        for guard in ["authorization", "unsigned", "host", "suite", "mixed", "hash", "size", "package", "version", "source", "source_version"]:
            with self.subTest(guard=guard):
                self.vendor_cache(candidate=candidate)
                pkg = self.cache["php8.4-cli"]
                if guard == "unsigned": pkg.candidate.origins[0].trusted = False
                elif guard == "host": pkg.candidate.origins[0].site = "unrelated.example"
                elif guard == "suite": pkg.candidate.origins[0].archive = "jammy"
                elif guard == "mixed": pkg.candidate.origins += Version(candidate).origins
                elif guard == "hash": pkg.candidate.record["SHA256"] = ""
                elif guard == "size": pkg.candidate.size = 0
                elif guard == "package": pkg.candidate.record["Package"] = ""
                elif guard == "version": pkg.candidate.record["Version"] = candidate + "+other"
                elif guard == "source": pkg.candidate.record["Source"] = "unrelated-source"
                elif guard == "source_version": pkg.candidate.record["Source"] += " (8.4.28-99)"
                with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=guard != "authorization"):
                    self.assertTrue(self.plan()["blocked_reasons"])

    def test_signed_packaging_fallback_keeps_transaction_guards(self):
        candidate = "8.4.28-4+debsuryorg24.04.1+packaging2+3"
        for guard in ["held", "dependency", "downgrade", "source", "core"]:
            with self.subTest(guard=guard):
                self.vendor_cache(candidate=candidate)
                pkg = self.cache["php8.4-cli"]
                if guard == "held": pkg._pkg.selected_state = apt_pkg.SELSTATE_HOLD
                elif guard == "dependency": self.cache.broken_count = 1
                elif guard == "downgrade": pkg.installed.version = "1:" + candidate
                elif guard == "source": pkg.candidate.source_name = "php-redis"
                elif guard == "core": pkg.candidate.source_version = candidate + "+other"
                with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
                    self.assertTrue(self.plan()["blocked_reasons"])

    def test_new_sury_requires_explicit_policy_adoption(self):
        self.vendor_cache()
        self.assertTrue(any("Review the new PHP repository" in reason for reason in self.plan()["blocked_reasons"]))
        with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
            self.assertEqual([], self.plan()["blocked_reasons"])

    def test_current_vendor_revision_requires_the_same_trust_and_authorization(self):
        for release, suite in [("22.04", "jammy"), ("24.04", "noble"), ("26.04", "resolute")]:
            self.vendor_cache("8.4.26-2+0~deb.sury.org.1+ubuntu"+release)
            for pkg in self.cache:
                pkg.candidate.version = "8.4.27-2+0~deb.sury.org.1+ubuntu"+release
                pkg.candidate.origins[0].archive = suite
            self.assertTrue(any("Review the new PHP repository" in r for r in self.plan()["blocked_reasons"]))
            with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
                self.assertEqual([], self.plan()["blocked_reasons"])
                self.cache["php8.4-cli"].candidate.origins[0].trusted = False
                self.assertTrue(any("trusted repository" in r for r in self.plan()["blocked_reasons"]))

    def test_current_vendor_revision_rejects_near_matches(self):
        for version in ["8.4.27-2+0~deb.sury.org.1+debian12",
                        "8.4.27-2+0~deb.sury.org.1+ubuntu25.10",
                        "8.4.27-2+0~deb.sury.org.2+ubuntu24.04",
                        "8.4.27-2+0~deb.sury.org.1+ubuntu24.04+local",
                        "8.4.27-2+0~debXsuryXorg.1+ubuntu24.04",
                        "8.4.27+0~deb.sury.org.1+ubuntu24.04"]:
            self.assertFalse(bridge["sury_revision"](version), version)

    def test_debsuryorg_updates_without_readoption(self):
        for release, suite in [("22.04", "jammy"), ("24.04", "noble"), ("26.04", "resolute")]:
            for epoch in ["", "1:"]:
                # Both a packaging-family transition and subsequent updates
                # use the existing authorization, including epoch versions.
                for installed in ["8.4.27-2+0~deb.sury.org.1+ubuntu" + release,
                                  "8.4.28-1+debsuryorg" + release + ".1+php+1"]:
                    candidate = epoch + "8.4.28-2+debsuryorg" + release + ".1+php+1"
                    with self.subTest(installed=epoch + installed, candidate=candidate):
                        self.vendor_cache(epoch + installed, candidate, suite)
                        with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
                            plan = self.plan()
                        self.assertEqual([], plan["blocked_reasons"])
                        self.assertEqual(4, len(plan["changes"]))

    def test_debsuryorg_revision_keeps_adoption_and_origin_guards(self):
        candidate = "8.4.28-3+debsuryorg24.04.1+1"
        self.vendor_cache(candidate=candidate)
        self.assertTrue(any("Review the new PHP repository" in r for r in self.plan()["blocked_reasons"]))
        for field, value in [("trusted", False), ("site", "unrelated.example"),
                             ("origin", "Unrelated vendor"), ("component", "universe"),
                             ("archive", "bookworm"), ("archive", "jammy"),
                             ("archive", "resolute")]:
            with self.subTest(field=field, value=value):
                self.vendor_cache(candidate=candidate)
                setattr(self.cache["php8.4-cli"].candidate.origins[0], field, value)
                with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
                    self.assertTrue(self.plan()["blocked_reasons"])

    def test_debsuryorg_shape_cannot_authorize_an_unpinned_key(self):
        import io
        import json
        import os
        import stat
        load = bridge["load_retained_catalog"]
        other_key = b"unrecognized signing key"
        original_open = open
        def read(path, *a, **k):
            if path == "/usr/share/keyrings/fluxo-sury-archive.gpg":
                return io.BytesIO(other_key)
            return original_open(path, *a, **k)
        self.vendor_cache(candidate="8.4.28-3+debsuryorg24.04.1+1")
        with tempfile.TemporaryDirectory() as directory:
            retained = pathlib.Path(directory) / "retained"
            retained.mkdir()
            # Even a policy authorization matching an unrecognized live key
            # cannot substitute for the existing pinned provider identity.
            (pathlib.Path(directory) / "authorization.json").write_text(json.dumps(dict(
                policy_hash="current", key_sha256=hashlib.sha256(other_key).hexdigest())))
            metadata = types.SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=0, st_size=100)
            with patch.dict(load.__globals__, open=read, private_directory=lambda p: None,
                            policy_hash=lambda: "current", REPOSITORY_AUTHORIZED=True), \
                    patch.object(os, "lstat", return_value=metadata):
                load(str(retained))
                self.assertFalse(load.__globals__["REPOSITORY_AUTHORIZED"])
                self.assertTrue(any("Review the new PHP repository" in r for r in self.plan()["blocked_reasons"]))

    def test_debsuryorg_revision_keeps_continuity_guards(self):
        candidate = "8.4.28-3+debsuryorg24.04.1+1"
        for installed in ["8.4.27-0ubuntu1", "8.4.27-2+debsuryorg22.04.1+php+1",
                          "8.4.27-2+debsuryorg26.04.1+php+1",
                          "8.4.27-2+debsuryorg12.1+php+1"]:
            with self.subTest(installed=installed):
                self.vendor_cache(installed, candidate)
                with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
                    self.assertTrue(any("continuity" in r for r in self.plan()["blocked_reasons"]))

    def test_debsuryorg_revision_keeps_transaction_guards(self):
        candidate = "8.4.28-3+debsuryorg24.04.1+1"
        for guard in ["held", "dependency", "downgrade", "source", "core", "mixed origins"]:
            with self.subTest(guard=guard):
                self.vendor_cache(candidate=candidate)
                pkg = self.cache["php8.4-cli"]
                if guard == "held":
                    pkg._pkg.selected_state = apt_pkg.SELSTATE_HOLD
                    reason = "is held"
                elif guard == "dependency":
                    self.cache.broken_count = 1
                    reason = "requires dependency changes"
                elif guard == "downgrade":
                    pkg.installed.version = "1:" + candidate
                    reason = "older than installed"
                elif guard == "source":
                    pkg.candidate.source_name = "unrelated-source"
                    reason = "changes source package"
                elif guard == "core":
                    pkg.candidate.source_version = "8.4.28-3+debsuryorg24.04.1+php+1"
                    reason = "not a consistent source revision"
                else:
                    pkg.candidate.origins += Version(candidate).origins
                    reason = "Ambiguous vendor origins"
                with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
                    self.assertTrue(any(reason in r for r in self.plan()["blocked_reasons"]))

    def test_ubuntu_to_vendor_crossover_blocked_after_adoption(self):
        self.vendor_cache("8.4.26-0ubuntu1")
        with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
            self.assertTrue(any("continuity" in reason for reason in self.plan()["blocked_reasons"]))

    def test_vendor_origin_wrong_host_or_debian_revision_blocked(self):
        self.vendor_cache()
        self.cache["php8.4-cli"].candidate.origins[0].site = "unrelated.example"
        self.cache["php8.4-fpm"].candidate.version = "8.4.27-1+0~20261021.57+debian12~1.gbp7636b9"
        with patch.dict(bridge["inspect"].__globals__, REPOSITORY_AUTHORIZED=True):
            self.assertGreaterEqual(len(self.plan()["blocked_reasons"]), 2)

    def test_preserved_extension_keeps_live_origins_honest_and_is_not_an_upgrade(self):
        old="6.2.3-1+ubuntu24.04.1+deb.sury.org+1"
        p=Package("php8.4-swoole",old,old,"php-swoole")
        for version in [p.installed,p.candidate]:
            version.origins=[]
            version.record={"Package":p.shortname}
        metadata=dict(name=p.shortname,version=old,architecture=p.installed.architecture,sha256="b"*64,size=100,control={"Source":"php-swoole (6.2.3-1)"},origins=[dict(origin="LP-PPA-ondrej-php",trusted=True)])
        self.cache.append(p)
        with patch.dict(bridge["package"].__globals__,REPOSITORY_AUTHORIZED=True,RETAINED_CATALOG=[metadata],RETAINED_DIRECTORY="/retained",retained_metadata=lambda v:metadata if v.source_name=="php-swoole" else None,verify_archive=lambda *a:None):
            plan=self.plan()
            extension=next(pkg for pkg in plan["packages"] if pkg["name"]==p.shortname)
            self.assertTrue(extension["preserved_extension"])
            self.assertEqual([],extension["origins"])
            self.assertEqual("",extension["sha256"])
            self.assertNotIn(p.shortname,[pkg["name"] for pkg in plan["changes"]])
            self.assertEqual([],plan["blocked_reasons"])
            # Without an authenticated record the exception disappears.
            with patch.dict(bridge["package"].__globals__,RETAINED_CATALOG=[]):
                self.assertTrue(self.plan()["blocked_reasons"])

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


class SuryRevisions(unittest.TestCase):
    def test_supported_families_releases_and_epochs(self):
        for release, suite in [("22.04", "jammy"), ("24.04", "noble"), ("26.04", "resolute")]:
            for epoch in ["", "1:"]:
                for revision in ["8.4.28-1+ubuntu" + release + ".1+deb.sury.org+1",
                                 "8.4.28-1+0~20261021.57+ubuntu" + release + "~1.gbp7636b9",
                                 "8.4.28-2+0~deb.sury.org.1+ubuntu" + release,
                                 "8.4.28-2+debsuryorg" + release + ".1+php+1",
                                 "8.4.28-3+debsuryorg" + release + ".1+1"]:
                    with self.subTest(version=epoch + revision):
                        self.assertTrue(bridge["sury_revision"](epoch + revision))
                        self.assertTrue(bridge["sury_revision"](epoch + revision, suite))
                        self.assertFalse(bridge["sury_revision"](epoch + revision, "bookworm"))
                        other_suite = "jammy" if suite != "jammy" else "noble"
                        self.assertFalse(bridge["sury_revision"](epoch + revision, other_suite))

    def test_debsuryorg_near_matches(self):
        for version in ["8.4.28-2+debsuryorg12.1+php+1",
                        "8.4.28-2+debsuryorg25.10.1+php+1",
                        "8.4.28-2+debsuryorg24x04.1+php+1",
                        "8.4.28-2+debsuryorg24.04+php+1",
                        "8.4.28-2+debsuryorg24.04.1+php",
                        "8.4.28-2+debsuryorg24.04.1+php+1+local",
                        "8.4.28-2+debsuryorg24.04.1+php+1\n",
                        "8.4.28+debsuryorg24.04.1+php+1",
                        "8.4.28-2+debsuryorg24.04.1+php8.4+1"]:
            with self.subTest(version=version):
                self.assertFalse(bridge["sury_revision"](version))


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
