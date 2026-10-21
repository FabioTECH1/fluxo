"""Source parsing and conflict-aware recovery; never modifies host APT files."""
import runpy
import pathlib
import sys
import unittest
from unittest.mock import patch

namespace = runpy.run_path(sys.argv[1], run_name="fixture")
exec(pathlib.Path(sys.argv[2]).read_text(), namespace)
rewrite = namespace["repo_rewrite"]

class Sources(unittest.TestCase):
    def test_native_esm_revisions_and_complete_vendor_priority(self):
        for version in ["8.3.6-0ubuntu0.24.04.13", "8.1.2-1ubuntu2.25+esm1", "7.4.3-4ubuntu2.29+esm7"]:
            self.assertTrue(namespace["repo_native_revision"](version))
        self.assertFalse(namespace["repo_native_revision"]("8.4.27-1+0~20261021.57+ubuntu24.04~1.gbp7636b9"))
        pin = namespace["repo_native_pin"]("8.3")
        self.assertEqual(3, pin.count("Pin-Priority: 990"))
        for origin in ["Ubuntu", "UbuntuESM", "UbuntuESMApps"]:
            self.assertIn("Pin: release o="+origin+"\n", pin)

    def test_custom_pin_fragments_cannot_lose_effect_by_sort_order(self):
        import os
        check=namespace["repo_check_custom_pins"]
        rule=b"Package: php8.3-fpm\nPin: version 8.3.6-0ubuntu0.24.04.15+esm1\nPin-Priority: -1\n"
        for name in ["aa-local.pref", "zz-local.pref", "zz.local.pref"]:
            with patch.dict(check.__globals__,repo_file=lambda p,**k:rule if p.endswith(name) else None),patch.object(os,"listdir",return_value=["fluxo-php-vendors",name]):
                with self.assertRaisesRegex(RuntimeError,"manual reconciliation"):
                    check(["8.3"])
        self.assertFalse(namespace["repo_pin_overlap"]("nginx* redis-server",["8.3"]))
        self.assertTrue(namespace["repo_pin_overlap"]("*",["8.3"]))
        self.assertTrue(namespace["repo_pin_overlap"]("src:php8.3:any",["8.3"]))
        standard=b"# Ubuntu Pro managed preference\n# Comment before first field\nPackage: *\nPin: release o=UbuntuESMApps\nPin-Priority: 510\n"
        with patch.dict(check.__globals__,repo_file=lambda p,**k:standard if p.endswith("ubuntu-pro-esm-apps") else None),patch.object(os,"listdir",return_value=["ubuntu-pro-esm-apps"]):
            check(["8.3"])
        with patch.dict(check.__globals__,repo_file=lambda p,**k:standard.replace(b"510",b"-1") if p.endswith("ubuntu-pro-esm-apps") else None),patch.object(os,"listdir",return_value=["ubuntu-pro-esm-apps"]):
            with self.assertRaisesRegex(RuntimeError,"manual reconciliation"): check(["8.3"])

    def test_list_preserves_unrelated_and_comments(self):
        source = b"# keep\ndeb http://archive.ubuntu.com/ubuntu noble main\ndeb [signed-by=/usr/share/keyrings/php.gpg] https://ppa.launchpadcontent.net/ondrej/php/ubuntu/ noble main\n"
        after, old, new = rewrite("php.list", source, "noble")
        self.assertEqual((1, 0), (old, new))
        self.assertTrue(after.startswith(b"# keep\ndeb http://archive.ubuntu.com/ubuntu noble main\n"))
        self.assertIn(b"# Disabled by Fluxo", after)
        self.assertEqual((after, 0, 0), rewrite("php.list", after, "noble"))

    def test_deb822_preserves_shared_stanzas(self):
        source = b"# Managed source comment\nTypes: deb\nURIs: https://ppa.launchpadcontent.net/ondrej/php/ubuntu/\nSuites: noble\nComponents: main\nSigned-By: /usr/share/keyrings/php.gpg\n\nTypes: deb\nURIs: http://archive.ubuntu.com/ubuntu\nSuites: noble noble-updates\nComponents: main universe\n"
        after, old, new = rewrite("php.sources", source, "noble")
        self.assertEqual((1, 0), (old, new))
        self.assertIn(b"Enabled: no", after)
        self.assertIn(b"Suites: noble noble-updates", after)
        self.assertEqual((after, 0, 0), rewrite("php.sources", after, "noble"))

    def test_mixed_stanza_wrong_suite_and_insecure_options_block(self):
        bad = [b"deb [trusted=yes] https://ppa.launchpadcontent.net/ondrej/php/ubuntu noble main\n",
               b"deb https://ppa.launchpadcontent.net/ondrej/php/ubuntu jammy main\n"]
        for raw in bad:
            with self.assertRaises(RuntimeError): rewrite("php.list", raw, "noble")
        mixed=b"Types: deb\nURIs: https://ppa.launchpadcontent.net/ondrej/php/ubuntu http://archive.ubuntu.com/ubuntu\nSuites: noble\nComponents: main\n"
        with self.assertRaises(RuntimeError): rewrite("php.sources",mixed,"noble")

    def test_existing_scoped_sury_reused_wrong_key_blocked(self):
        raw=b"Types: deb\nURIs: https://packages.sury.org/php/\nSuites: noble\nComponents: main\nSigned-By: /usr/share/keyrings/fluxo-sury-archive.gpg\n"
        self.assertEqual((raw,0,1),rewrite("php.sources",raw,"noble"))
        with self.assertRaises(RuntimeError):rewrite("php.sources",raw.replace(b"fluxo-sury-archive",b"another-key"),"noble")

    def test_deb822_casefold_and_duplicate_detection(self):
        source=b"tYpEs: deb\nuRiS: https://ppa.launchpadcontent.net/ondrej/php/ubuntu/\nsUiTeS: noble\ncOmPoNeNtS: main\neNaBlEd: yes\n"
        after, old, new = rewrite("php.sources",source,"noble")
        self.assertEqual((1,0),(old,new))
        self.assertIn(b"Enabled: no",after)
        self.assertEqual((after,0,0),rewrite("php.sources",after,"noble"))
        with self.assertRaises(RuntimeError):rewrite("php.sources",source+b"URIs: https://packages.sury.org/php/\n","noble")
        bad=source.replace(b"ppa.launchpadcontent.net/ondrej/php/ubuntu/",b"packages.sury.org/php/").replace(b"noble",b"jammy")
        with self.assertRaises(RuntimeError):rewrite("php.sources",bad,"noble")

    def test_stage_main_and_parts_do_not_collide(self):
        import tempfile
        import os
        staged={}
        original=namespace["repo_stage"]
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(original.__globals__,repo_file=lambda p,**k:b"main source" if p=="/etc/apt/sources.list" else b"parts source",repo_sync=lambda path,data,*a,**k:staged.update({path:data}),repo_refresh=lambda *a:None), patch.object(os,"listdir",return_value=["main.list"]):
                args=original(dict(changes=[]),directory,b"key")
            self.assertEqual(b"main source",staged[directory+"/sources.list"])
            self.assertEqual(b"parts source",staged[directory+"/sources/main.list"])
            self.assertIn("Dir::Etc::sourcelist="+directory+"/sources.list",args)

    def test_restore_preserves_private_and_zero_modes(self):
        import base64
        import types
        import os
        for mode in [0o600,0o000]:
            item=dict(path="source",before=base64.b64encode(b"before secret").decode(),after=base64.b64encode(b"after secret").decode(),before_mode=mode,before_uid=0,before_gid=0)
            calls=[]
            restore=namespace["repo_restore"]
            with patch.dict(restore.__globals__,repo_file=lambda *a,**k:b"after secret",repo_sync=lambda *a,**k:calls.append(a)), patch.object(os,"lstat",return_value=types.SimpleNamespace(st_mode=mode,st_uid=0,st_gid=0)):
                restore(dict(files=[item]),b"")
            self.assertEqual(("source",b"before secret",mode,0,0),calls[0])

    def test_orphan_json_partial_does_not_block_commit(self):
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            target=pathlib.Path(directory)/"journal.json"
            pathlib.Path(str(target)+".partial").write_text("uncommitted orphan")
            (pathlib.Path(directory)/".fluxo-state-orphan").write_text("uncommitted unique orphan")
            namespace["atomic_json"](str(target),dict(state="pending"))
            namespace["atomic_json"](str(target),dict(state="committed"))
            self.assertEqual('{"state": "committed"}',target.read_text())

    def test_inventory_drift_blocks_before_source_publication(self):
        import os
        apply=namespace["repository_apply"]
        plan=dict(configuration_id="reviewed",package_inventory=[dict(name="php8.4-cli",version="old")])
        with patch.dict(apply.__globals__,repo_configuration=lambda:dict(configuration_id="reviewed"),repo_inventory=lambda cache:[dict(name="php8.4-cli",version="changed")],repo_sync=lambda *a,**k:self.fail("published before validating reviewed inventory")), patch.object(namespace["apt"],"Cache",return_value=None), patch.object(os.path,"exists",return_value=False):
            with self.assertRaisesRegex(RuntimeError,"changed since repository review"):
                apply(dict(plan=plan,plan_id="review"),"/private",b"key")

    def test_restore_refuses_operator_edit_before_any_write(self):
        import base64
        item=dict(path="source",before=base64.b64encode(b"before").decode(),after=base64.b64encode(b"after").decode())
        restore=namespace["repo_restore"]
        with patch.dict(restore.__globals__,repo_file=lambda *a,**k:b"operator edit",repo_sync=lambda *a,**k:self.fail("overwrote operator edit")):
            with self.assertRaises(RuntimeError):restore(dict(files=[item]),b"")

if __name__ == "__main__":
    unittest.main(argv=[sys.argv[0]])
