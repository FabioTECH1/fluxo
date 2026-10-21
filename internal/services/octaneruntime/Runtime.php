<?php
namespace Fluxo\OctaneMaintenance;

final class Runtime
{
    public static function overlay($app): array
    {
        if (!function_exists('posix_geteuid') || posix_geteuid() === 0) {
            throw new \RuntimeException('Fluxo Octane must run as the application user');
        }
        $config = $app['config']->get('octane');
        if (!is_array($config)) { throw new \RuntimeException('Octane configuration unavailable'); }
        if (!in_array($config['server'] ?? '', ['swoole', 'roadrunner'], true)) { return $config; }
        $manifest = json_decode(file_get_contents(__DIR__.'/compatibility.json'), true, 16, JSON_THROW_ON_ERROR);
        $vendor = $app->basePath('vendor/laravel/octane');
        if (\Composer\InstalledVersions::getReference('laravel/octane') !== $manifest['octane_reference']) {
            throw new \RuntimeException('This Octane release is not qualified for Fluxo runtime maintenance');
        }
        $roadrunner = ($config['server'] ?? '') === 'roadrunner';
        $sources = $roadrunner ? $manifest['roadrunner']['sources'] : $manifest['sources'];
        foreach ($sources as $file => $hash) {
            if (!hash_equals($hash, hash_file('sha256', $vendor.'/'.$file))) {
                throw new \RuntimeException('Octane source differs from the qualified maintenance integration');
            }
        }
        if ($roadrunner) {
            $qualified = $manifest['roadrunner'];
            $version = \Composer\InstalledVersions::getVersion('laravel/octane');
            if (!in_array($version, [$qualified['octane_version'], $qualified['octane_version'].'.0'], true)) {
                throw new \RuntimeException('This Octane version is not qualified for Fluxo RoadRunner maintenance');
            }
            $binary = realpath($app->basePath('rr'));
            $identity = $qualified['binaries'][php_uname('m')] ?? null;
            if (PHP_OS_FAMILY !== 'Linux' || !is_array($identity) || !$binary || !is_file($binary) ||
                filesize($binary) !== $identity['size'] || !hash_equals($identity['sha256'], hash_file('sha256', $binary))) {
                throw new \RuntimeException('RoadRunner binary or architecture is not qualified for Fluxo maintenance; an approved official release is required');
            }
            return $config;
        }
        if (!extension_loaded('swoole') || phpversion('swoole') !== $manifest['swoole_version']) {
            throw new \RuntimeException('This native Swoole release is not qualified for Fluxo maintenance');
        }
        $swoole = $config['swoole'] ?? [];
        $command = $swoole['command'] ?? 'swoole-server';
        if (!in_array($command, ['swoole-server', __DIR__.'/swoole-server.php'], true) || !empty($swoole['php_options']) ||
            !empty($swoole['ssl']) || ($swoole['mode'] ?? 2) !== 2 || !empty($swoole['options']['daemonize']) ||
            !empty($swoole['options']['enable_coroutine']) || !empty($swoole['options']['user']) || !empty($swoole['options']['group'])) {
            throw new \RuntimeException('Custom Swoole launch settings require manual maintenance');
        }
        $config['swoole']['command'] = __DIR__.'/swoole-server.php';
        $assets = [];
        foreach (['start.php','Runtime.php','swoole-server.php','compatibility.json'] as $file) {
            $assets[$file] = hash_file('sha256', __DIR__.'/'.$file);
        }
        $config['fluxo_maintenance'] = ['schema'=>1, 'assets'=>$assets];
        $app['config']->set('octane', $config);
        return $config;
    }
}
