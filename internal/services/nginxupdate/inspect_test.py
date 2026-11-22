import importlib.util
import types
import unittest
from pathlib import Path
import apt_pkg

spec = importlib.util.spec_from_file_location('nginx_inspect', Path(__file__).with_name('inspect.py'))
subject = importlib.util.module_from_spec(spec)
spec.loader.exec_module(subject)
apt_pkg.init()


def version(text, trusted=True, downloadable=True):
    origin = types.SimpleNamespace(origin='Ubuntu', archive='noble-updates', site='archive.ubuntu.com', component='main', trusted=trusted)
    return types.SimpleNamespace(version=text, architecture='amd64', downloadable=downloadable, origins=[origin])


def inspect(installed='1.24.0-2ubuntu7.15', candidate='1.24.0-2ubuntu7.21', held=False, **kwargs):
    pkg = types.SimpleNamespace(name='nginx-core', installed=version(installed), candidate=version(candidate, **kwargs) if candidate else None, _pkg=types.SimpleNamespace(selected_state=apt_pkg.SELSTATE_HOLD if held else apt_pkg.SELSTATE_INSTALL))
    return subject.inspect({'nginx-core:amd64': pkg}, apt_pkg.version_compare, 'nginx-core:amd64')


class InspectionTests(unittest.TestCase):
    def test_distro_revision_epoch_and_tilde_use_actual_libapt(self):
        for before, after in [('1.24.0-2ubuntu7.15', '1.24.0-2ubuntu7.21'), ('1.24.0-1', '1:1.24.0-1'), ('1.24.0~rc1-1', '1.24.0-1'), ('1.24.0-9', '1.24.0-10')]:
            self.assertEqual(inspect(before, after)['state'], 'update_available')
    def test_current_under_effective_policy(self):
        self.assertEqual(inspect(candidate='1.24.0-2ubuntu7.15')['state'], 'current')
    def test_pinned_effective_candidate_is_used_instead_of_highest_visible_version(self):
        pkg = types.SimpleNamespace(name='nginx', installed=version('1.24.0-1'), candidate=version('1.24.0-1'), versions=[version('1.99.0-1'),version('1.24.0-1')], _pkg=types.SimpleNamespace(selected_state=0))
        self.assertEqual(subject.inspect({'nginx':pkg},apt_pkg.version_compare,'nginx')['state'],'current')

    def test_hold_is_not_applicable_update(self):
        self.assertEqual(inspect(held=True)['state'], 'held')
    def test_missing_untrusted_nondownloadable_and_downgrade(self):
        for kwargs in [dict(candidate=None), dict(trusted=False), dict(downloadable=False), dict(candidate='1.24.0-1')]:
            self.assertEqual(inspect(**kwargs)['state'], 'unavailable')
    def test_status_only_candidate_is_not_current(self):
        v = version('1.24.0-2ubuntu7.15');v.origins=[]
        pkg = types.SimpleNamespace(name='nginx', installed=v, candidate=v, _pkg=types.SimpleNamespace(selected_state=0))
        self.assertEqual(subject.inspect({'nginx':pkg},apt_pkg.version_compare,'nginx')['state'],'unavailable')
    def test_owner_variants_and_unsupported(self):
        for name in ['nginx','nginx-core','nginx-full','nginx-extras','nginx:arm64']:
            pkg=types.SimpleNamespace(name=name.split(':')[0],installed=version('1.2-1'),candidate=version('1.2-1'),_pkg=types.SimpleNamespace(selected_state=0))
            self.assertEqual(subject.inspect({name:pkg},apt_pkg.version_compare,name)['state'],'current')
        for name in ['manual-nginx','nginx;bad','nginx/unknown']:
            with self.assertRaises(ValueError):subject.inspect({},apt_pkg.version_compare,name)
    def test_source_host_drops_credentials(self):
        v=version('1.2-2');v.origins[0].site='user:private@example.test:443'
        pkg=types.SimpleNamespace(name='nginx',installed=version('1.2-1'),candidate=v,_pkg=types.SimpleNamespace(selected_state=0))
        self.assertEqual(subject.inspect({'nginx':pkg},apt_pkg.version_compare,'nginx')['sources'][0]['site'],'example.test')

if __name__=='__main__':unittest.main()
