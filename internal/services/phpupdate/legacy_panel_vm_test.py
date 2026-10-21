"""Guarded old-panel Scheduler handoff acceptance; no package changes.

Arguments: current Fluxo binary, test harness built from tag v0.4.35 with
internal/server/legacy_panel_fixture_test.go copied into that checkout.
"""
import concurrent.futures
import json
import os
import pathlib
import select
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
import urllib.request
import uuid


@unittest.skipUnless(os.environ.get("FLUXO_PHP_UPDATE_VM_TEST") == "1" and os.geteuid() == 0 and socket.gethostname() == "fluxo-php-patch-test", "guarded reusable VM only")
class LegacyPanelBoundary(unittest.TestCase):
    def test_real_v035_scheduler_pipes_private_tmp_and_older_upgrade(self):
        current, old = map(pathlib.Path, sys.argv[1:3])
        binary = pathlib.Path('/usr/local/bin/fluxo')
        unit = pathlib.Path('/etc/systemd/system/fluxo.service')
        version_file = pathlib.Path('/var/lib/fluxo-legacy-panel-test-version')
        self.assertFalse(version_file.exists(), 'another fixture owns this test')
        self.assertNotEqual(subprocess.run(['systemctl','is-active','--quiet','fluxo']).returncode, 0, 'preserve an active panel')
        saved_binary = (binary.read_bytes(), binary.stat().st_mode & 0o777) if binary.exists() else None
        saved_unit = unit.read_bytes() if unit.exists() else None
        root = pathlib.Path(tempfile.mkdtemp(prefix='fluxo-legacy-panel-acceptance-', dir='/var/lib'))
        app = pathlib.Path('/home/fluxo/legacy-panel-acceptance-' + str(uuid.uuid4()))
        app.mkdir(mode=0o755)
        environment = {**os.environ, 'FLUXO_ENV':'prod', 'FLUXO_DATA_DIR':str(root)}
        children = []
        pool = concurrent.futures.ThreadPoolExecutor(max_workers=3)

        def run(*args, **kwargs):
            return subprocess.run(args, text=True, capture_output=True, timeout=20, **kwargs)

        def request():
            req=urllib.request.Request('http://127.0.0.1:19695/api/v1/sites/1/crons/1910051/run',method='POST')
            try:
                with urllib.request.urlopen(req, timeout=25) as response: return response.status, response.read().decode()
            except urllib.error.HTTPError as error: return error.code, error.read().decode()

        def start(version):
            version_file.write_text(version)
            self.assertEqual(run('systemctl','start','fluxo').returncode,0)
            deadline=time.monotonic()+10
            while time.monotonic()<deadline:
                try:
                    with urllib.request.urlopen('http://127.0.0.1:19695/health', timeout=1): return
                except OSError: time.sleep(.1)
            self.fail('old panel did not start')

        def helper():
            process=subprocess.Popen([str(current),'scheduler-installer-lease',str(os.getpid())],env=environment,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            children.append(process)
            deadline=time.monotonic()+13
            while time.monotonic()<deadline:
                if select.select([process.stdout],[],[],.1)[0]:
                    line=process.stdout.readline().strip()
                    if line.startswith('READY '): return process,line.split()[1]
                    if not line: break
            output,error=process.communicate(timeout=3)
            self.fail('no installer readiness: '+output+error)

        def release(process, token):
            result=run(str(current),'scheduler-installer-release',token)
            self.assertEqual(result.returncode,0,result.stderr)
            output,error=process.communicate(timeout=5)
            self.assertEqual(process.returncode,0,output+error)

        try:
            shutil.copyfile(old,binary);binary.chmod(0o755)
            unit.write_text('[Unit]\nDescription=Fluxo legacy Scheduler acceptance fixture\n[Service]\nExecStart=/usr/local/bin/fluxo\nUser=root\nPrivateTmp=true\nEnvironment=FLUXO_ENV=prod\nEnvironment=FLUXO_TEST_LEGACY_PANEL_DIR='+str(root)+'\n')
            self.assertEqual(run('systemctl','daemon-reload').returncode,0)
            start('0.4.35')
            db=sqlite3.connect(root/'fluxo.db')
            db.execute("INSERT INTO sites(id,domain,path,php_version,app_type) VALUES(1,'legacy-panel.test',?,'8.4','laravel')",(str(app),))
            command='php8.4 '+str(app)+'/artisan schedule:run'
            db.execute("INSERT INTO crons(id,site_id,name,expression,command,user) VALUES(1910051,1,'Laravel Scheduler','* * * * *',?,'fluxo')",(command,));db.commit()
            script='''<?php
file_put_contents(__DIR__.'/started','yes');
file_put_contents('/tmp/fluxo-private-scheduler','alive');
for($i=0;$i<8;$i++){echo "tick$i\\n"; usleep(500000);}
$p=pcntl_fork();if($p>0)exit(0);posix_setsid();$p=pcntl_fork();if($p>0)exit(0);
fclose(STDOUT);fclose(STDERR);
pcntl_exec('/bin/sh',['-c','sleep 2; test -f /tmp/fluxo-private-scheduler && echo done > '.escapeshellarg(__DIR__.'/completed')]);
'''
            (app/'artisan').write_text(script)
            self.assertEqual(run('chown','-R','fluxo:fluxo',str(app)).returncode,0)
            work=pool.submit(request)
            deadline=time.monotonic()+5
            while not (app/'started').exists() and time.monotonic()<deadline: time.sleep(.05)
            self.assertTrue((app/'started').exists())
            before=time.monotonic()
            process,token=helper()
            self.assertGreater(time.monotonic()-before,4)
            self.assertTrue((app/'completed').exists(),'PrivateTmp child lost its resources')
            status,body=work.result(timeout=1)
            self.assertEqual(status,200,body)
            self.assertIn('tick7',body,'old output pipe was interrupted')
            self.assertEqual(request()[0],409,'new manual request bypassed the exclusive lease')
            # Older panels can enqueue checks while their execution is held.
            operation=str(uuid.uuid4());now=int(time.time())
            db.execute("INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES(?,'check','8.4','queued',?,?)",(operation,now,now));db.commit()
            self.assertEqual(run('systemctl','stop','fluxo').returncode,0)
            self.assertNotEqual(run(str(current),'scheduler-installer-verify',env=environment).returncode,0,'post-stop queued operation was snapshotted')
            db.execute('DELETE FROM php_update_operations WHERE id=?',(operation,));db.commit()
            self.assertEqual(run(str(current),'scheduler-installer-verify',env=environment).returncode,0)
            release(process,token)
            # Upgrade eligibility is separate from unproved old admission.
            for history in [True,False]:
                if not history: db.execute('DELETE FROM crons');db.commit()
                start('0.4.33')
                process,token=helper()
                journal=json.loads(pathlib.Path('/var/lib/fluxo-scheduler/state.json').read_text())
                self.assertIsNone(journal['panel_proof'],'older panel received fabricated proof')
                self.assertEqual(run('systemctl','stop','fluxo').returncode,0)
                release(process,token)
            db.close()
            print('Preserved real v0.4.35 handoff evidence:',root,app)
        finally:
            for child in children:
                if child.poll() is None:
                    journal=json.loads(pathlib.Path('/var/lib/fluxo-scheduler/state.json').read_text())
                    lease=journal.get('upgrade')
                    if lease and lease['pid']==child.pid: run(str(current),'scheduler-installer-release',lease['id'])
                    child.wait(timeout=10)
                if child.stdout:child.stdout.close()
                if child.stderr:child.stderr.close()
            pool.shutdown(wait=True)
            run('systemctl','stop','fluxo')
            if saved_binary:binary.write_bytes(saved_binary[0]);binary.chmod(saved_binary[1])
            else:binary.unlink(missing_ok=True)
            if saved_unit is None:unit.unlink(missing_ok=True)
            else:unit.write_bytes(saved_unit)
            version_file.unlink(missing_ok=True)
            run('systemctl','daemon-reload')


if __name__=='__main__':unittest.main(argv=[sys.argv[0]],verbosity=2)
