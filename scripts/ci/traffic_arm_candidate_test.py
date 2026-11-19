"""Offline guard/snapshot tests; never provisions or connects to a machine."""
import json
import hashlib
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

import traffic_arm_acceptance as subject
import traffic_native_acceptance as traffic


class HostedGuardTests(unittest.TestCase):
    def setUp(self):
        self.source = 'a' * 40
        self.env = {'FLUXO_TRAFFIC_ARM_CANDIDATE': '1', 'GITHUB_ACTIONS': 'true',
                    'RUNNER_ENVIRONMENT': 'github-hosted', 'RUNNER_ARCH': 'ARM64',
                    'GITHUB_REF': subject.BRANCH, 'GITHUB_SHA': self.source}

    def guarded(self, *, uid=0, machine='aarch64', source=None, dpkg='arm64', existing=False, extra=None, release='24.04'):
        environment = dict(self.env, **(extra or {}))
        os_release = 'ID=ubuntu\nVERSION_ID="' + release + '"\n'
        with patch.dict(os.environ, environment, clear=True), \
             patch.object(subject.os, 'geteuid', return_value=uid), \
             patch.object(subject.platform, 'machine', return_value=machine), \
             patch.object(subject.subprocess, 'check_output', return_value=(source or self.source) + '\n'), \
             patch.object(subject.base, 'run', return_value={'stdout': dpkg}), \
             patch.object(Path, 'read_text', return_value=os_release), \
             patch.object(subject.os.path, 'lexists', return_value=existing):
            return subject.hosted_profile()

    def test_exact_official_native_context_is_accepted(self):
        self.assertEqual(self.guarded(), ('arm64', '24.04'))

    def test_separate_amd64_optin_requires_actual_arch_and_noble(self):
        amd = {'FLUXO_TRAFFIC_ARM_CANDIDATE': '0', 'FLUXO_TRAFFIC_AMD_CANDIDATE': '1', 'RUNNER_ARCH': 'X64'}
        self.assertEqual(self.guarded(machine='x86_64', dpkg='amd64', extra=amd), ('amd64', '24.04'))
        for case in ({'machine': 'aarch64', 'dpkg': 'arm64'},
                     {'machine': 'x86_64', 'dpkg': 'arm64'},
                     {'machine': 'x86_64', 'dpkg': 'amd64', 'release': '26.04'}):
            with self.subTest(case=case), self.assertRaises(RuntimeError):
                self.guarded(extra=amd, **case)

    def test_root_optin_ref_arch_source_and_empty_host_are_all_required(self):
        cases = [{'uid': 1000}, {'machine': 'x86_64'}, {'dpkg': 'amd64'},
                 {'source': 'b' * 40}, {'existing': True},
                 {'extra': {'GITHUB_REF': 'refs/heads/main'}},
                 {'extra': {'GITHUB_REF': 'refs/heads/test/traffic-native-acceptance'}},
                 {'extra': {'RUNNER_ENVIRONMENT': 'self-hosted'}},
                 {'extra': {'GITHUB_ACTIONS': 'false'}},
                 {'extra': {'FLUXO_TRAFFIC_ARM_CANDIDATE': '0'}},
                 {'extra': {'FLUXO_TRAFFIC_AMD_CANDIDATE': '1'}}]
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
            self.assertEqual(result['api'], {})
            self.assertNotIn('token', json.dumps(result))

    def test_snapshot_includes_api_support_floor_settings_rules_and_metrics(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'traffic').mkdir()
            with sqlite3.connect(root / 'traffic/analytics.db') as db:
                db.executescript('CREATE TABLE sources(site,generation,path,start,dev,ino,offset); '
                    'CREATE TABLE hourly(site,hour,requests,pageviews,not_found,other_4xx,server_errors,bytes,time_ms); '
                    'CREATE TABLE api_meta(support_ns); CREATE TABLE api_settings(site,prefixes,routes,effective_ns); '
                    'CREATE TABLE api_hourly(site,hour,requests);')
                db.execute('INSERT INTO sources VALUES(1,"g","/private/log.jsonl",1,2,3,456)')
                db.execute('INSERT INTO api_meta VALUES(123)')
                db.execute('INSERT INTO api_settings VALUES(1,?, ?, 124)', ('["/api"]', '[]'))
                db.executemany('INSERT INTO api_hourly VALUES(?,?,?)', [(1, 3600, 4), (2, 3600, 9)])
            with patch.object(subject, 'DATA', root):
                result = subject.analytics_snapshot(1)
            self.assertEqual(result['api']['api_meta'], [(123,)])
            self.assertEqual(result['api']['api_hourly'], [(1, 3600, 4)])
            self.assertEqual(result['api']['api_settings'], [(1, '["/api"]', '[]', 124)])

    def test_actual_nginx_metadata_expectations_use_weighted_time_and_status_classes(self):
        rows = [{'status': 200, 'bytes': 20, 'request_time': .001},
                {'status': 302, 'bytes': 30, 'request_time': .002},
                {'status': 403, 'bytes': 40, 'request_time': .003},
                {'status': 503, 'bytes': 50, 'request_time': .010}]
        expected = {'requests': 4, 'response_bytes': 140, 'status_1xx': 0,
                    'status_2xx': 1, 'status_3xx': 1, 'status_4xx': 1, 'status_5xx': 1,
                    'avg_request_time_ms': 4.0}
        self.assertEqual(traffic.measured_api_metrics(rows), expected)
        traffic.assert_api_metrics(expected, expected, 'API')
        with self.assertRaises(RuntimeError):
            traffic.assert_api_metrics(dict(expected, avg_request_time_ms=5), expected, 'API')

    def test_nginx_metadata_reader_ignores_partial_writes_and_unrelated_paths(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            folder = root / 'traffic/logs/1'
            folder.mkdir(parents=True)
            (folder / 'g.jsonl').write_text('{"path":"/api/users/1","bytes":4}\n'
                                          '{"path":"/unrelated","bytes":9}\n{"path":')
            with patch.object(traffic, 'DATA', root):
                self.assertEqual(traffic.nginx_records(1, {'/api/users/1'}),
                                 [{'path': '/api/users/1', 'bytes': 4}])

    def test_previous_published_fixture_requires_both_exact_artifacts_and_amd64(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for filename, payload in (('fluxo', b'published-binary'), ('install.sh', b'published-installer')):
                (root / filename).write_bytes(payload)
            qualification = {'tag': 'v0.4.52-base-test.1', 'immutable': True,
                             'source': 'fc25f9b3ea579787e4b027b8053ada316e5c77d8',
                             'assets': {'fluxo-linux-amd64': {'sha256': hashlib.sha256(b'published-binary').hexdigest()},
                                        'install.sh': {'sha256': hashlib.sha256(b'published-installer').hexdigest()}}}
            proof = root / 'qualification.json'
            proof.write_text(json.dumps(qualification))
            with patch.object(subject, 'PREVIOUS_RELEASE', proof):
                self.assertEqual(subject.verify_previous_build(root, 'amd64')['tag'], qualification['tag'])
                with self.assertRaises(RuntimeError):
                    subject.verify_previous_build(root, 'arm64')
                (root / 'fluxo').write_bytes(b'changed')
                with self.assertRaises(RuntimeError):
                    subject.verify_previous_build(root, 'amd64')

    def test_vhost_readiness_waits_for_every_prior_worker_identity(self):
        old = {10: 'old-10', 11: 'old-11'}
        with patch.object(subject, 'process_start', side_effect=lambda pid: {10: None, 11: 'old-11'}[pid]):
            self.assertFalse(subject.children_retired(old))
        with patch.object(subject, 'process_start', side_effect=lambda pid: {10: None, 11: 'reused-pid'}[pid]):
            self.assertTrue(subject.children_retired(old))


if __name__ == '__main__':
    unittest.main()
