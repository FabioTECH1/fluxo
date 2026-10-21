<?php
// Adapted from laravel/octane v2.22.0 bin/swoole-server (MIT).
// Copyright (c) Taylor Otwell. See THIRD_PARTY_LICENSE.txt.
// Fluxo owns admission and timer lifecycle; upstream owns request/task handling.

use Laravel\Octane\RequestContext;
use Laravel\Octane\Swoole\Handlers\OnManagerStart;
use Laravel\Octane\Swoole\Handlers\OnServerStart;
use Laravel\Octane\Swoole\Handlers\OnWorkerStart;
use Laravel\Octane\Swoole\ServerStateFile;
use Laravel\Octane\Swoole\SwooleExtension;
use Laravel\Octane\Swoole\WorkerState;
use Swoole\Http\Server;
use Swoole\Timer;

ini_set('display_errors', 'stderr');
if (!function_exists('posix_geteuid') || posix_geteuid() === 0) { throw new RuntimeException('Non-root runtime required'); }
$octane = realpath(($_SERVER['APP_BASE_PATH'] ?? '').'/vendor/laravel/octane');
if (!$octane) { throw new RuntimeException('Octane vendor path unavailable'); }


require_once $octane.'/src/Stream.php';

require $octane.'/fixes/fix-symfony-dd.php';

$bootstrap = fn ($serverState) => require $octane.'/bin/bootstrap.php';

/*
|--------------------------------------------------------------------------
| Create The Swoole Server
|--------------------------------------------------------------------------
|
| First, we will load the server state file from disk. This file contains
| various information we need to boot Swoole such as the configuration
| and application name. We can use this data to start up our server.
|
*/

$serverState = json_decode(file_get_contents(
    $serverStateFile = $_SERVER['argv'][1]
), true)['state'];

$server = require $octane.'/bin/createSwooleServer.php';

$timerTable = require $octane.'/bin/createSwooleTimerTable.php';

/*
|--------------------------------------------------------------------------
| Handle Server & Manager Start
|--------------------------------------------------------------------------
|
| The following callbacks manage the master process and manager process
| start events. These handlers primarily are responsible for writing
| the process ID to the server state file so we can remember them.
|
*/

// Gate and active-callback tracking are shared across the master/worker fork.
$gate = new Swoole\Atomic(0);
$active = new Swoole\Table(4096);
$active->column('pid', Swoole\Table::TYPE_INT);
$active->column('identity', Swoole\Table::TYPE_STRING, 64);
$active->create();
$identity = static function (int $pid) {
    $path = '/proc/'.$pid.'/stat';
    $raw = @file_get_contents($path);
    if ($raw === false) { return file_exists('/proc/'.$pid) ? false : null; }
    $end = strrpos($raw, ')');
    if ($end === false) { return false; }
    $fields = preg_split('/\\s+/', trim(substr($raw, $end+1)));
    if (count($fields) < 20) { return false; }
    return $fields[0] === 'Z' ? null : $fields[19];
};
$tickTimer = null; $watchdogTimer = null; $drainTimer = null;
$server->on('start', function (Server $server) use ($bootstrap, $serverState, $serverStateFile, $timerTable, &$tickTimer, &$watchdogTimer) {
    $bootstrap($serverState);
    (new ServerStateFile($serverStateFile))->writeProcessIds($server->master_pid, $server->manager_pid);
    $extension = new SwooleExtension;
    $extension->setProcessName($serverState['appName'], 'master process');
    if ($serverState['octaneConfig']['tick'] ?? true) {
        $tickTimer = Timer::tick(1000, static fn () => $server->task('octane-tick'));
    }
    $limit = $serverState['octaneConfig']['max_execution_time'] ?? 0;
    if ($limit > 0) {
        $watchdogTimer = Timer::tick(1000, static function () use ($extension, $timerTable, $limit, $server) {
            (new Laravel\Octane\Swoole\Actions\EnsureRequestsDontExceedMaxExecutionTime($extension, $timerTable, $limit, $server))();
        });
    }
});
$server->on('beforeShutdown', function () use ($gate, $active, $identity, &$tickTimer, &$watchdogTimer, &$drainTimer) {
    $gate->set(1);
    $known = array_filter([$tickTimer, $watchdogTimer], static fn ($id) => $id !== null);
    $unknown = false;
    foreach (Timer::list() as $id) { if (!in_array($id, $known, true)) { $unknown = true; } }
    if ($tickTimer !== null) { Timer::clear($tickTimer); $tickTimer = null; }
    // Retain the original watchdog and its request timestamps until every
    // admitted callback finishes, including termination/response handling.
    // Unknown timers or uncertain process identity leave shutdown blocked.
    $drainTimer = Timer::tick(50, function () use ($active, $identity, $unknown, &$watchdogTimer, &$drainTimer) {
        foreach ($active as $key => $row) {
            $now = $identity($row['pid']);
            if ($now === null || is_string($now) && $now !== $row['identity']) { $active->del($key); }
        }
        $empty = count($active) === 0;
        if ($empty && !$unknown) {
            if ($watchdogTimer !== null) { Timer::clear($watchdogTimer); $watchdogTimer = null; }
            Timer::clear($drainTimer); $drainTimer = null;
        }
    });
    if ($unknown) { fwrite(STDERR, "Fluxo maintenance blocked by an unrecognized master timer\n"); }
});

$server->on('managerstart', function () use ($serverState, $octane) {
    // Don't bootstrap entire application before server / worker start. Otherwise, files can't be gracefully reloaded... #632
    require_once $octane.'/src/Swoole/Handlers/OnManagerStart.php';
    require_once $octane.'/src/Swoole/SwooleExtension.php';

    (new OnManagerStart(
         new SwooleExtension, $serverState['appName']
    ))();
});

/*
|--------------------------------------------------------------------------
| Handle Worker Start
|--------------------------------------------------------------------------
|
| Swoole will start multiple worker processes and the following callback
| will handle their state events. When a worker starts we will create
| a new Octane worker and inform it to start handling our requests.
|
| We will also create a "workerState" variable which will maintain state
| and allow us to access the worker and client from the callback that
| will handle incoming requests. Basically this works like a cache.
|
*/

require_once $octane.'/bin/WorkerState.php';

$workerState = new WorkerState;

$workerState->cacheTable = require $octane.'/bin/createSwooleCacheTable.php';
$workerState->timerTable = $timerTable;
$workerState->tables = require $octane.'/bin/createSwooleTables.php';

$server->on('workerstart', fn (Server $server, $workerId) =>
    (fn ($basePath) => (new OnWorkerStart(
        new SwooleExtension, $basePath, $serverState, $workerState
    ))($server, $workerId))($bootstrap($serverState))
);

/*
|--------------------------------------------------------------------------
| Handle Incoming Requests
|--------------------------------------------------------------------------
|
| The following callback will handle all incoming requests plus send them
| the worker. The worker will send the request through the application
| and ask the client to send the response back to the Swoole server.
|
*/

$server->on('request', function ($request, $response) use ($server, $workerState, $serverState, $gate, $active, $identity) {
    $pid = getmypid(); $key = (string)$pid;
    $token = $identity($pid);
    $registered = is_string($token) && $active->set($key, ['pid'=>$pid,'identity'=>$token]);
    // Publish activity BEFORE the atomic gate read. A read of open linearizes
    // admission before closure, and its row already exists for the drain poll.
    // A callback registering after closure sees closed and never enters Laravel.
    // No application-level mutex can be stranded by a watchdog-killed worker.
    if (!$registered || $gate->get() !== 0) {
        if ($registered) { $active->del($key); }
        $response->status(503); $response->end('Runtime maintenance'); return;
    }
    try {

    $workerState->lastRequestTime = microtime(true);

    if ($workerState->timerTable) {
        $workerState->timerTable->set($workerState->workerId, [
            'worker_pid' => $workerState->workerPid,
            'time' => time(),
            'fd' => $request->fd,
        ]);
    }

    $workerState->worker->handle(...$workerState->client->marshalRequest(new RequestContext([
        'swooleRequest' => $request,
        'swooleResponse' => $response,
        'publicPath' => $serverState['publicPath'],
        'octaneConfig' => $serverState['octaneConfig'],
    ])));

    if ($workerState->timerTable) {
        $workerState->timerTable->del($workerState->workerId);
    }
    } finally {
        $active->del($key);
    }
});

/*
|--------------------------------------------------------------------------
| Handle Tasks
|--------------------------------------------------------------------------
|
| Swoole tasks can be used to offload concurrent work onto a group of
| background processes which handle the work in isolation and with
| separate application state. We should handle these tasks below.
|
*/

$server->on('task', fn (Server $server, int $taskId, int $fromWorkerId, $data) =>
    $data === 'octane-tick'
            ? $workerState->worker->handleTick()
            : $workerState->worker->handleTask($data)
);

$server->on('finish', fn (Server $server, int $taskId, $result) => $result);

/*
|--------------------------------------------------------------------------
| Handle Worker & Server Shutdown
|--------------------------------------------------------------------------
|
| The following callbacks handle the master and worker shutdown events so
| we can clean up any state, including the server state file. An event
| will be dispatched by the worker so the developer can take action.
|
*/

$server->on('workerstop', function () use ($workerState) {
    if ($workerState->tickTimerId) {
        Timer::clear($workerState->tickTimerId);
    }

    $workerState->worker->terminate();
});

$server->start();
