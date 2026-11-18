"""Bounded read-only controller for the isolated native fixture's safe progress."""
import json
from pathlib import Path
import subprocess
import sys
import time

root, wanted, seconds, unit = sys.argv[1:]
began=time.monotonic();end=began+int(seconds)
last=None
while time.monotonic()<end:
    try: progress=json.loads((Path(root)/'progress.json').read_bytes())
    except (OSError,ValueError):progress={}
    safe={k:progress.get(k) for k in ('stage','checkpoints','finished','passed','error','elapsed_seconds','installer_milestones')}
    snapshot=json.dumps(safe,sort_keys=True)
    if snapshot!=last:
        print(snapshot,flush=True);last=snapshot
    if wanted=='finished' and progress.get('finished'):
        raise SystemExit(0 if progress.get('passed') else 1)
    if wanted in progress.get('checkpoints',[]):raise SystemExit(0)
    if progress.get('finished'):raise SystemExit('Native fixture failed before requested checkpoint')
    active=subprocess.run(['systemctl','is-active','--quiet',unit],timeout=5).returncode==0
    if not active and time.monotonic()-began>10:
        raise SystemExit('Native controller exited without requested checkpoint; retained report identifies last stage')
    time.sleep(2)
print(json.dumps({'controller_timeout_seconds':int(seconds),'requested_checkpoint':wanted,'last_stage':progress.get('stage')}),flush=True)
(Path(root)/'controller-timeout.json').write_text(json.dumps({'seconds':int(seconds),'checkpoint':wanted,'last_stage':progress.get('stage')})+'\n')
raise SystemExit(1)
