"""Offline guard/snapshot tests; never provisions or connects to a machine."""
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

import traffic_arm_acceptance as subject


class HostedGuardTests(unittest.TestCase):
    def setUp(self):
        self.source = 'a' * 40
        self.env = {'FLUXO_TRAFFIC_ARM_CANDIDATE': '1', 'GITHUB_ACTIONS': 'true',
                    'RUNNER_ENVIRONMENT': 'github-hosted', 'RUNNER_ARCH': 'ARM64',
                    'GITHUB_REF': subject.BRANCH, 'GITHUB_SHA': self.source}

    def guarded(self, *, uid=0, machine='aarch64', source=None, dpkg='arm64', existing=False, extra=None):
        environment = dict(self.env, **(extra or {}))
        release = 'ID=ubuntu\nVERSION_ID="24.04"\n'
        with patch.dict(os.environ, environment, clear=True), \
             patch.object(subject.os, 'geteuid', return_value=uid), \
             patch.object(subject.platform, 'machine', return_value=machine), \
             patch.object(subject.subprocess, 'check_output', return_value=(source or self.source) + '\n'), \
             patch.object(subject.base, 'run', return_value={'stdout': dpkg}), \
             patch.object(Path, 'read_text', return_value=release), \
             patch.object(subject.os.path, 'lexists', return_value=existing):
            return subject.hosted_profile()

    def test_exact_official_native_context_is_accepted(self):
        self.assertEqual(self.guarded(), '24.04')

    def test_root_optin_ref_arch_source_and_empty_host_are_all_required(self):
        cases = [{'uid': 1000}, {'machine': 'x86_64'}, {'dpkg': 'amd64'},
                 {'source': 'b' * 40}, {'existing': True},
                 {'extra': {'GITHUB_REF': 'refs/heads/main'}},
                 {'extra': {'RUNNER_ENVIRONMENT': 'self-hosted'}},
                 {'extra': {'GITHUB_ACTIONS': 'false'}},
                 {'extra': {'FLUXO_TRAFFIC_ARM_CANDIDATE': '0'}}]
        for case in cases:
            with self.subTest(case=case), self.assertRaises(RuntimeError):
                self.guarded(**case)

    def test_local_failure_does_not_execute_a_command(self):
        with patch.dict(os.environ, {}, clear=True), \
             patch.object(subject.os, 'geteuid', return_value=1000), \
             patch.object(subject.subprocess, 'check_output') as command:
            with self.assertRaises(RuntimeError):
                subject.hosted_profile()
            command.assert_not_called()

    def test_snapshot_projects_durable_cursor_and_totals_without_user_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'traffic').mkdir()
            with sqlite3.connect(root / 'traffic/analytics.db') as db:
                db.executescript('CREATE TABLE sources(site,generation,path,start,dev,ino,offset); '
                    'CREATE TABLE hourly(site,hour,requests,pageviews,not_found,other_4xx,server_errors,bytes,time_ms);')
                db.execute('INSERT INTO sources VALUES(?,?,?,?,?,?,?)', (1, 'generation', '/private/log.jsonl', 1, 2, 3, 456))
                db.execute('INSERT INTO hourly VALUES(?,?,?,?,?,?,?,?,?)', (1, 3600, 4, 2, 1, 0, 0, 100, 25.0))
            with patch.object(subject, 'DATA', root):
                result = subject.analytics_snapshot(1)
            self.assertEqual(result['generation'], 'generation')
            self.assertEqual(result['cursor'], ('/private/log.jsonl', 1, 2, 3, 456))
            self.assertEqual(result['aggregates'], [(3600, 4, 2, 1, 0, 0, 100, 25.0)])
            self.assertNotIn('token', json.dumps(result))


if __name__ == '__main__':
    unittest.main()
