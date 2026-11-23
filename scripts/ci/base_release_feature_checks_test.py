import copy
import tempfile
from pathlib import Path
import unittest
import base_release_feature_checks as probe


class FeatureOracleTests(unittest.TestCase):
    def test_history_reads_actual_ranges_and_refuses_truncation_or_fake_coverage(self):
        class API:
            def __init__(self, change=None):
                self.change = change
                self.calls = []
            def request(self, method, path):
                self.calls.append((method, path))
                if path == '/traffic-analytics':
                    return dict(retention_days=0, master_enabled=False)
                period = path.split('range=')[1]
                bucket = dict(metrics=dict(requests=3), collection='enabled')
                report = dict(buckets=[bucket], totals=dict(requests=3))
                result = dict(report, range=period, retention_days=0,
                    bucket_seconds=86400 if period == '90d' else 604800 if period == '365d' else 3600,
                    **{'from': '2026-11-23T00:00:00Z', 'to': '2026-11-23T12:00:00Z'},
                    api=copy.deepcopy(report))
                if self.change:
                    self.change(result)
                return result
        good = API()
        result = probe.traffic_history_observations(good, 1)
        self.assertEqual(set(result['ranges']), {'today', '90d', '365d', 'all'})
        self.assertTrue(all(method == 'GET' for method, _ in good.calls))
        for change in [lambda r: r.update(bucket_seconds=0),
                       lambda r: r.update(retention_days=30),
                       lambda r: r.update(buckets=r['buckets'] * 721),
                       lambda r: r['totals'].update(requests=4),
                       lambda r: r['buckets'][0].update(collection='unknown')]:
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                probe.traffic_history_observations(API(change), 1)

    def test_current_and_failed_metadata_are_distinct(self):
        result = dict(running_adoption='not_verified', stale=False, last_successful_check=1000, snapshot=dict(state='current',package='nginx',architecture='amd64',installed_version='1.24-1',candidate_version='1.24-1'))
        saved = dict(format=1,next_check=87400)
        self.assertTrue(probe.check_discovery_truth(result,saved,1010)['fresh_success'])
        failed = dict(result,error='Repository refresh failed',stale=True,notice=False)
        self.assertFalse(probe.check_discovery_truth(failed,dict(format=1,next_check=4600),1010)['fresh_success'])
        failed['stale']=False
        with self.assertRaises(RuntimeError):probe.check_discovery_truth(failed,saved,1010)
        with self.assertRaises(RuntimeError):probe.check_discovery_truth(result,dict(format=1,next_check=8000),1010)

    def test_exec_trace_has_real_metadata_and_refuses_mutation_or_unknown(self):
        raw='execve("/usr/bin/apt-get", ["/usr/bin/apt-get", "-o", "APT::Update::Error-Mode=any", "update"], 0x123 /* 4 vars */) = 0'
        self.assertTrue(probe.trace_commands(raw)[0]['apt_metadata_refresh'])
        for bad in [raw.replace('"update"','"install"'), 'execve("/bin/systemctl", ["systemctl", "restart", "nginx"], 0x123) = 0', 'execve("/usr/sbin/nginx", ["nginx", "-s", "reload"], 0x123) = 0', 'execve("/usr/bin/kill", ["kill", "-HUP", "12"], 0x123) = 0','execve(incomplete']:
            with self.subTest(bad=bad),self.assertRaises(RuntimeError):probe.trace_commands(bad)
        self.assertEqual(probe.trace_commands(raw.replace('= 0','= -1 ENOENT')),[])

    def test_network_preservation_is_actual_settings_not_revision(self):
        state=dict(managed=True,pending=False,protection=dict(enabled=False,username=''),redirects=[dict(id='r',source='/base-release-preservation',destination='/new',status=302)],wildcards=[],revision='old')
        sha=probe.network_digest(state);state['revision']='new'
        self.assertEqual(sha,probe.network_digest(state))
        for field in ['redirects','managed','pending']:
            changed=copy.deepcopy(state);changed[field]=[] if field=='redirects' else not changed[field]
            if field=='redirects':self.assertNotEqual(sha,probe.network_digest(changed))
            else:
                with self.assertRaises(RuntimeError):probe.network_digest(changed)

    def test_all_new_markers_required_not_removed_footer(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'binary';path.write_bytes('\n'.join(probe.MARKERS).encode())
            self.assertTrue(probe.public_ui_markers(path)['all_present'])
            path.write_bytes('\n'.join(probe.MARKERS[:-1]).encode())
            with self.assertRaises(RuntimeError):probe.public_ui_markers(path)

if __name__=='__main__':unittest.main()
