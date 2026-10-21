<?php
// Fixed root-owned launcher. All application boot and hooks run as fluxo.
if (!function_exists('posix_geteuid') || posix_geteuid() === 0) { throw new RuntimeException('Non-root application startup required'); }
require __DIR__.'/Runtime.php';
require getcwd().'/vendor/autoload.php';
$app = require getcwd().'/bootstrap/app.php';
$kernel = $app->make(Illuminate\Contracts\Console\Kernel::class);
$kernel->bootstrap();
Fluxo\OctaneMaintenance\Runtime::overlay($app);
if (count($argv) !== 3 || $argv[1] !== '--host=127.0.0.1' || !preg_match('/^--port=([0-9]{4,5})$/D', $argv[2], $match) || (int)$match[1] < 2000 || (int)$match[1] > 65535) {
    throw new RuntimeException('Unsupported managed Octane startup arguments');
}
exit($kernel->call('octane:start', ['--host'=>'127.0.0.1','--port'=>$match[1]], new Symfony\Component\Console\Output\ConsoleOutput));
