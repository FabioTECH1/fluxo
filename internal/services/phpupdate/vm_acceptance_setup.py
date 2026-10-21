"""Provision isolated fixtures only in Fluxo's reusable Ubuntu acceptance VM.

Never run on a production host. Original application/package fixtures are kept.
The signing key authenticates disposable test revisions, not production PHP.
"""
import datetime
import email.utils
import hashlib
import json
import os
from pathlib import Path
import pwd
import shutil
import socket
import subprocess

if os.geteuid() != 0 or socket.gethostname() != "fluxo-php-patch-test" or os.environ.get("FLUXO_PHP_UPDATE_VM_TEST") != "1":
    raise SystemExit("Explicit disposable acceptance VM opt-in required")


def run(*args, cwd=None, binary=False):
    result = subprocess.run(args, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    if result.returncode:
        raise RuntimeError(str(args[:3]) + ": " + result.stderr.decode(errors="replace")[-3000:])
    return result.stdout if binary else result.stdout.decode()


user = pwd.getpwnam("fluxo")
app = Path("/home/fluxo/php-recovery-acceptance")
if not app.exists():
    shutil.copytree("/home/fluxo/php-patch-acceptance", app, symlinks=True)
    for directory, children, files in os.walk(app):
        os.chown(directory, user.pw_uid, user.pw_gid)
        for name in children + files:
            os.chown(Path(directory) / name, user.pw_uid, user.pw_gid, follow_symlinks=False)


def application(*args):
    return run("sudo", "-u", "fluxo", "/usr/bin/env", "HOME=/home/fluxo", "PATH=/usr/bin:/bin", "/usr/bin/php8.4", *args, cwd=app)


print("Preparing separate Laravel/Horizon acceptance application", flush=True)
application("/usr/bin/composer", "require", "laravel/horizon:^5.0", "laravel/octane:^2.0", "--no-interaction", "--no-progress")
env = app / ".env"
settings = dict(APP_ENV="local", APP_NAME="FluxoRecoveryAcceptance", DB_CONNECTION="sqlite", DB_DATABASE=str(app / "database/database.sqlite"), CACHE_STORE="file", SESSION_DRIVER="file", QUEUE_CONNECTION="redis", REDIS_CLIENT="phpredis",
                REDIS_HOST="127.0.0.1", REDIS_PORT="6379", REDIS_PREFIX="fluxo_php_recovery_", HORIZON_PREFIX="fluxo_php_recovery_horizon:",
                PHP_RECOVERY_QUEUE="fluxo-php-recovery", RECOVERY_FAST_TERMINATION="true")
lines = env.read_text().splitlines()
lines = [line for line in lines if line.split("=", 1)[0] not in settings]
env.write_text("\n".join(lines) + "\n" + "\n".join(k + "=" + v for k, v in settings.items()) + "\n")
os.chown(env, user.pw_uid, user.pw_gid)
horizon = app / "config/horizon.php"
horizon.write_text("""<?php
return [
 'use' => 'default', 'prefix' => env('HORIZON_PREFIX'),
 'fast_termination' => env('RECOVERY_FAST_TERMINATION', true),
 'defaults' => ['recovery' => ['connection' => 'redis', 'queue' => [env('PHP_RECOVERY_QUEUE')],
  'balance' => false, 'maxProcesses' => 1, 'maxTime' => 0, 'maxJobs' => 0, 'memory' => 128, 'tries' => 3, 'timeout' => 120, 'nice' => 0]],
 'environments' => ['local' => ['recovery' => []]],
];
""")
os.chown(horizon, user.pw_uid, user.pw_gid)
job = app / "app/Jobs/RecoveryDrainJob.php"
job.parent.mkdir(exist_ok=True)
job.write_text("""<?php
namespace App\\Jobs;
use Illuminate\\Contracts\\Queue\\ShouldQueue;
use Illuminate\\Foundation\\Queue\\Queueable;
class RecoveryDrainJob implements ShouldQueue {
 use Queueable;
 public $timeout = 120;
 public function __construct(public int $seconds, public string $label) {}
 public function handle(): void {
  $record = fn ($event) => file_put_contents(storage_path('app/recovery-events.jsonl'),
   json_encode(['label'=>$this->label,'event'=>$event,'time'=>microtime(true),'pid'=>getmypid()])."\\n", FILE_APPEND|LOCK_EX);
  $record('started'); $until=microtime(true)+$this->seconds;
  while(microtime(true)<$until) { usleep(50000); }
  $record('finished');
 }
}
""")
os.chown(job.parent, user.pw_uid, user.pw_gid)
os.chown(job, user.pw_uid, user.pw_gid)
application("artisan", "config:clear", "--no-interaction")

print("Preparing authenticated disposable old/new package repository", flush=True)
repository = Path("/var/lib/fluxo-php-recovery-fixture-repo")
repository.mkdir(mode=0o755, exist_ok=True)
for source in Path("/var/tmp/fluxo-php-fixture").glob("*.deb"):
    shutil.copyfile(source, repository / source.name)
    os.chmod(repository / source.name, 0o644)
records = []
for package in sorted(repository.glob("*.deb")):
    data = package.read_bytes()
    records.append(run("dpkg-deb", "-f", str(package)).rstrip() + "\nFilename: ./" + package.name + "\nSize: " + str(len(data)) + "\nSHA256: " + hashlib.sha256(data).hexdigest() + "\n")
(repository / "Packages").write_text("\n".join(records) + "\n")
date = email.utils.format_datetime(datetime.datetime.now(datetime.timezone.utc), usegmt=True)
data = (repository / "Packages").read_bytes()
release = "Origin: LP-PPA-ondrej-php\nLabel: Fluxo disposable PHP recovery fixture\nSuite: fluxo-test\nCodename: fluxo-test\nDate: " + date + "\nArchitectures: amd64\nSHA256:\n " + hashlib.sha256(data).hexdigest() + " " + str(len(data)) + " Packages\n"
(repository / "Release").write_text(release)
keyhome = Path("/var/lib/fluxo-php-recovery-fixture-key")
keyhome.mkdir(mode=0o700, exist_ok=True)
if not run("gpg", "--homedir", str(keyhome), "--batch", "--list-secret-keys").strip():
    run("gpg", "--homedir", str(keyhome), "--batch", "--pinentry-mode", "loopback", "--passphrase", "", "--quick-generate-key", "Fluxo Disposable Recovery VM Fixture", "rsa2048", "sign", "0")
public = run("gpg", "--homedir", str(keyhome), "--batch", "--export", binary=True)
Path("/usr/share/keyrings/fluxo-php-recovery-fixture.gpg").write_bytes(public)
run("gpg", "--homedir", str(keyhome), "--batch", "--yes", "--digest-algo", "SHA256", "--clearsign", "--output", str(repository / "InRelease"), str(repository / "Release"))
Path("/etc/apt/sources.list.d/fluxo-php-recovery-fixture.list").write_text("deb [signed-by=/usr/share/keyrings/fluxo-php-recovery-fixture.gpg] file:" + str(repository) + " ./\n")
run("apt-get", "-o", "APT::Update::Error-Mode=any", "update")
print(json.dumps({"application": str(app), "repository": str(repository), "fixture_packages": len(list(repository.glob('*.deb')))}), flush=True)
