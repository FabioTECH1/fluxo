<?php
// Fixed inspection program, executed only as the non-root application user.
// It emits an allowlist and hashes; never environment values or application data.
if (!function_exists('posix_geteuid') || posix_geteuid() === 0) { throw new RuntimeException('Non-root application inspection required'); }
ob_start(static fn ($output) => '', 4096);
require 'vendor/autoload.php';
// Deliberately do not require bootstrap/app.php or bootstrap a console kernel.
// A fresh container loads only normal environment/configuration files, without
// registering or booting application providers (which may write or need a DB).
// Provider-mutated/custom bootstrap configuration is unsupported; live state
// must match this configuration-only descriptor before maintenance is admitted.
$app = new Illuminate\Foundation\Application(realpath(getcwd()));
(new Illuminate\Foundation\Bootstrap\LoadEnvironmentVariables)->bootstrap($app);
(new Illuminate\Foundation\Bootstrap\LoadConfiguration)->bootstrap($app);
if (!$app->configurationIsCached()) {
    // Reproduce OctaneServiceProvider::mergeConfigFrom without running it.
    $defaults = require base_path('vendor/laravel/octane/config/octane.php');
    $app['config']->set('octane', array_merge($defaults, $app['config']->get('octane', [])));
}
require '/usr/local/lib/fluxo/octane/v1/Runtime.php';
Fluxo\OctaneMaintenance\Runtime::overlay($app);
$version = Composer\InstalledVersions::getVersion('laravel/octane');
if (!is_string($version) || !str_starts_with($version, '2.')) { throw new RuntimeException('Octane 2.x is required for managed maintenance'); }
$base = realpath(base_path());
$storage = realpath(storage_path());
$vendor = realpath(base_path('vendor/laravel/octane'));
if (!$base || !$storage || !$vendor) { throw new RuntimeException('Octane application paths are unavailable'); }
$config = config('octane');
$backend = $config['server'] ?? '';
if (!in_array($backend, ['swoole', 'roadrunner', 'frankenphp'], true)) { throw new RuntimeException('Unsupported Octane backend'); }
$files = ['src/Commands/StartCommand.php', 'src/Commands/Concerns/InteractsWithServers.php'];
$native = '';
$grace = 'runtime default';
$worker = '';
$binary = '';
$extension = null;
if ($backend === 'swoole') {
    $swoole = $config['swoole'] ?? [];
    if (!extension_loaded('swoole') && !extension_loaded('openswoole')) { throw new RuntimeException('Swoole extension unavailable'); }
    if (($swoole['ssl'] ?? false) || !empty($swoole['php_options']) || ($swoole['command'] ?? '') !== '/usr/local/lib/fluxo/octane/v1/swoole-server.php' || ($swoole['mode'] ?? 2) !== 2 || !empty($swoole['options']['daemonize'])) {
        throw new RuntimeException('Custom Swoole launch options require manual maintenance');
    }
    $grace = isset($swoole['options']['max_wait_time']) ? (string) $swoole['options']['max_wait_time'].' seconds' : $grace;
    $paths = [];
    foreach (file('/proc/self/maps', FILE_IGNORE_NEW_LINES) as $mapping) {
        $fields = preg_split('/\s+/', trim($mapping), 6);
        if (isset($fields[5]) && str_ends_with($fields[5], '/swoole.so')) { $paths[$fields[5]] = true; }
    }
    if (count($paths) !== 1) { throw new RuntimeException('Native Swoole mapping identity unavailable'); }
    $file = array_key_first($paths);
    if (realpath($file) !== $file) { throw new RuntimeException('Unsupported native Swoole path'); }
    $native = hash_file('sha256', $file);
    $extension = ['name'=>'swoole','version'=>phpversion('swoole'),'file'=>$file,'sha256'=>$native];
    $files = array_merge($files, ['src/Commands/StartSwooleCommand.php', 'src/Swoole/ServerProcessInspector.php', 'bin/swoole-server', 'bin/createSwooleServer.php']);
} elseif ($backend === 'roadrunner') {
    // Octane's generated .rr.yaml stub is empty. Additional RR plugins or YAML
    // overrides need their own lifecycle contract and are intentionally refused.
    $yaml = base_path('.rr.yaml');
    if (is_file($yaml) && trim(file_get_contents($yaml, false, null, 0, 65537)) !== '') { throw new RuntimeException('Custom RoadRunner YAML requires manual maintenance'); }
    $worker = base_path('vendor/bin/roadrunner-worker');
    if (($config['roadrunner']['command'] ?? 'vendor/bin/roadrunner-worker') !== 'vendor/bin/roadrunner-worker') { throw new RuntimeException('Custom RoadRunner worker requires manual maintenance'); }
    $binary = realpath(base_path('rr'));
    if (!$binary || !is_file($binary) || filesize($binary) > 128 * 1024 * 1024) { throw new RuntimeException('RoadRunner executable unavailable'); }
    $native = hash_file('sha256', $binary);
    $files = array_merge($files, ['src/Commands/StartRoadRunnerCommand.php', 'src/RoadRunner/ServerProcessInspector.php']);
}
$hashes = [];
foreach ($files as $file) { $hashes[$file] = hash_file('sha256', $vendor.'/'.$file); }
$descriptor = ['backend'=>$backend, 'directory'=>$base, 'storage'=>$storage, 'vendor'=>$vendor, 'app_name'=>(string) config('app.name'),
    'fingerprint'=>hash('sha256', json_encode([$config, $hashes, $native, Composer\InstalledVersions::getReference('laravel/octane')], JSON_THROW_ON_ERROR)),
    'grace'=>$grace, 'rr_binary'=>$binary, 'rr_worker'=>$worker, 'rr_sha256'=>$backend === 'roadrunner' ? $native : '', 'extension'=>$extension];
$result = ['descriptor'=>$descriptor];
if (($argv[1] ?? '') === 'runtime') {
    $path = config('octane.state_file', storage_path('logs/octane-server-state.json'));
    $raw = file_get_contents($path, false, null, 0, 65537);
    if (!is_string($raw) || strlen($raw)>65536) { throw new RuntimeException('Octane state unavailable or too large'); }
    $state = json_decode($raw, true, 64, JSON_THROW_ON_ERROR);
    if (hash('sha256', json_encode($state['state']['octaneConfig'] ?? null, JSON_THROW_ON_ERROR)) !== hash('sha256', json_encode($config, JSON_THROW_ON_ERROR))) {
        throw new RuntimeException('Running Octane configuration differs from application configuration');
    }
    $result['master_pid'] = (int) ($state['masterProcessId'] ?? 0);
    $result['manager_pid'] = (int) ($state['managerProcessId'] ?? 0);
    $result['host'] = $state['state']['host'] ?? '';
    $result['port'] = (int) ($state['state']['port'] ?? 0);
}
ob_end_clean();
echo 'FLUXO_RUNTIME_JSON:'.json_encode($result, JSON_THROW_ON_ERROR)."\n";
