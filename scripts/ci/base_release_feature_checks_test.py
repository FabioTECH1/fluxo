import copy
import tempfile
from pathlib import Path
import unittest
import base_release_feature_checks as probe


class FeatureOracleTests(unittest.TestCase):
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
