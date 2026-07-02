<script setup lang="ts">
import { ref } from 'vue'
import { useTheme } from '@fluxo/composables/useTheme'
import { version as appVersion } from '../../package.json'

const { theme } = useTheme()
const mobileMenuOpen = ref(false)
const copied = ref(false)
const activeTab = ref<'install' | 'login' | 'upgrade'>('install')

function toggleTheme() {
  if (theme.value === 'dark') {
    theme.value = 'light'
  } else if (theme.value === 'light') {
    theme.value = 'system'
  } else {
    theme.value = 'dark'
  }
}

function scrollTo(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth' })
  mobileMenuOpen.value = false
}

async function copyInstall() {
  let cmd = 'curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash'
  if (activeTab.value === 'login') {
    cmd = 'sudo cat /home/fluxo/.fluxo_credentials'
  } else if (activeTab.value === 'upgrade') {
    cmd = `curl -fsSL https://fluxo.fottify.com/install.sh | FLUXO_VERSION=v${appVersion} sudo -E bash`
  }
  try {
    await navigator.clipboard.writeText(cmd)
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch { }
}
</script>

<template>
  <div class="bg-white dark:bg-gray-950 text-gray-900 dark:text-gray-100 antialiased transition-colors"
    style="font-family: Inter, system-ui, sans-serif">
    <header
      class="fixed top-0 inset-x-0 z-50 border-b border-gray-200 dark:border-gray-800 bg-white/80 dark:bg-gray-950/80 backdrop-blur-md">
      <div class="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 h-16 flex items-center justify-between">
        <a href="/" class="flex items-center gap-2 font-bold text-xl tracking-tight">
          <img src="/logo.png" alt="fluxo" class="h-8 w-8 object-cover" />
          <span>fluxo</span>
        </a>
        <nav class="hidden md:flex items-center gap-8 text-sm font-medium text-gray-600 dark:text-gray-400">
          <button @click="scrollTo('features')"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors">Features</button>
          <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
            @click="mobileMenuOpen = false">Demo</a>
          <button @click="scrollTo('install')"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors">Install</button>
          <a href="https://github.com/FabioTECH1/fluxo" target="_blank"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors">GitHub</a>
          <button @click="toggleTheme"
            class="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors text-lg leading-none"
            :title="'Theme: ' + theme">
            {{ theme === 'light' ? '☀' : theme === 'dark' ? '☽' : '☿' }}
          </button>
          <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
            class="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-blue-600 text-white font-semibold text-sm hover:bg-blue-700 transition-colors shadow-sm">
            Live Demo →
          </a>
        </nav>
        <div class="flex items-center gap-1 md:hidden">
          <button @click="toggleTheme"
            class="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors text-lg leading-none"
            :title="'Theme: ' + theme">
            {{ theme === 'light' ? '☀' : theme === 'dark' ? '☽' : '☿' }}
          </button>
          <button @click="mobileMenuOpen = !mobileMenuOpen"
            class="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors">
            <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </button>
        </div>
      </div>
      <div v-if="mobileMenuOpen"
        class="md:hidden border-t border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950 px-4 py-4 space-y-4 text-sm">
        <button @click="scrollTo('features')"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">Features</button>
        <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">Demo</a>
        <button @click="scrollTo('install')"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">Install</button>
        <a href="https://github.com/FabioTECH1/fluxo" target="_blank"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">GitHub</a>

        <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
          class="block w-full text-center px-4 py-2.5 rounded-lg bg-blue-600 text-white font-semibold">Live Demo →</a>
      </div>
    </header>

    <section class="pt-32 pb-20 px-4 sm:px-6 lg:px-8 max-w-6xl mx-auto text-center">
      <div
        class="inline-flex items-center gap-2 px-4 py-1.5 rounded-full bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 text-sm font-medium mb-8 border border-blue-200 dark:border-blue-800">
        🚀 v{{ appVersion }} — Source-Available
      </div>
      <h1 class="text-4xl sm:text-5xl lg:text-6xl font-extrabold tracking-tight leading-tight">
        Deploy & manage your servers
        <span class="text-blue-600 dark:text-blue-400"> without the hassle</span>
      </h1>
      <p class="mt-6 text-lg sm:text-xl text-gray-600 dark:text-gray-400 max-w-2xl mx-auto leading-relaxed">
        Fluxo is a self-hosted control panel inspired by Laravel Forge. Manage Nginx sites, PHP-FPM pools, SSL
        certificates, databases, daemons, cron jobs, and zero-downtime deployments — all from a clean dashboard.
      </p>
      <div class="mt-10 flex flex-col sm:flex-row items-center justify-center gap-4">
        <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
          class="w-full sm:w-auto inline-flex items-center justify-center gap-2 px-8 py-3.5 rounded-xl bg-blue-600 text-white font-semibold text-lg hover:bg-blue-700 transition-colors shadow-lg shadow-blue-600/20 dark:shadow-blue-600/10">
          Live Demo →
        </a>
        <button @click="scrollTo('install')"
          class="w-full sm:w-auto inline-flex items-center justify-center gap-2 px-8 py-3.5 rounded-xl border border-gray-300 dark:border-gray-700 text-gray-700 dark:text-gray-300 font-semibold text-lg hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
          Install Now
        </button>
        <a href="https://github.com/FabioTECH1/fluxo" target="_blank"
          class="w-full sm:w-auto inline-flex items-center justify-center gap-2 px-8 py-3.5 rounded-xl border border-gray-300 dark:border-gray-700 text-gray-700 dark:text-gray-300 font-semibold text-lg hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
          <svg class="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
            <path
              d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z" />
          </svg>
          Source Code
        </a>
      </div>
    </section>

    <section id="features" class="py-20 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-900/50">
      <div class="max-w-6xl mx-auto">
        <div class="text-center mb-16">
          <h2 class="text-3xl sm:text-4xl font-bold tracking-tight">Everything you need to run your servers</h2>
          <p class="mt-4 text-lg text-gray-600 dark:text-gray-400 max-w-2xl mx-auto">No monthly fees, no third-party
            dependencies. Run Fluxo on any VPS and get a full-featured control panel.</p>
        </div>
        <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-6">
          <div
            class="p-6 rounded-xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-blue-100 dark:bg-blue-900/40 flex items-center justify-center text-blue-600 dark:text-blue-400 text-lg mb-4">
              ⚡</div>
            <h3 class="font-semibold text-lg mb-2">Zero-Downtime Deployments</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Deploy new releases alongside the
              running site and swap symlinks atomically. Rollback in one click.</p>
          </div>
          <div
            class="p-6 rounded-xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-green-100 dark:bg-green-900/40 flex items-center justify-center text-green-600 dark:text-green-400 text-lg mb-4">
              🔒</div>
            <h3 class="font-semibold text-lg mb-2">One-Click SSL</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Issue and renew Let's Encrypt
              certificates for your domains from the dashboard. No SSH commands needed.</p>
          </div>
          <div
            class="p-6 rounded-xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-purple-100 dark:bg-purple-900/40 flex items-center justify-center text-purple-600 dark:text-purple-400 text-lg mb-4">
              🗄️</div>
            <h3 class="font-semibold text-lg mb-2">Database Management</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Create and manage MySQL and PostgreSQL
              databases, users, and permissions — with an integrated grants editor.</p>
          </div>
          <div
            class="p-6 rounded-xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-amber-100 dark:bg-amber-900/40 flex items-center justify-center text-amber-600 dark:text-amber-400 text-lg mb-4">
              🔄</div>
            <h3 class="font-semibold text-lg mb-2">GitHub Integration</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Connect your GitHub account, select
              repositories, inject deploy keys, and trigger deployments automatically on push.</p>
          </div>
          <div
            class="p-6 rounded-xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-rose-100 dark:bg-rose-900/40 flex items-center justify-center text-rose-600 dark:text-rose-400 text-lg mb-4">
              🛡️</div>
            <h3 class="font-semibold text-lg mb-2">Firewall & System</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Manage UFW rules, systemd daemons, cron
              jobs, SSH keys, and monitor system metrics — CPU, memory, disk.</p>
          </div>
          <div
            class="p-6 rounded-xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-cyan-100 dark:bg-cyan-900/40 flex items-center justify-center text-cyan-600 dark:text-cyan-400 text-lg mb-4">
              📜</div>
            <h3 class="font-semibold text-lg mb-2">Real-Time Logs & Terminal</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Tail Nginx, PHP-FPM, and application
              logs via WebSocket. Execute commands directly from the web terminal.</p>
          </div>
        </div>
      </div>
    </section>

    <!-- Provisioning Section -->
    <section class="py-24 px-4 sm:px-6 lg:px-8 max-w-6xl mx-auto border-t border-gray-100 dark:border-gray-900">
      <div class="grid lg:grid-cols-2 gap-8 lg:gap-16 items-center">
        <!-- Graphic -->
        <div class="relative flex justify-center items-center">
          <div class="absolute -inset-4 bg-blue-500/10 dark:bg-blue-500/5 rounded-3xl blur-2xl"></div>

          <div
            class="relative w-full max-w-md bg-gray-950 dark:bg-black rounded-2xl border border-gray-850 shadow-2xl p-4 sm:p-6 font-mono text-xs text-gray-400 space-y-4 text-left">
            <!-- Window header -->
            <div class="flex items-center gap-1.5 pb-2 border-b border-gray-900">
              <span class="w-2 h-2 rounded-full bg-rose-500"></span>
              <span class="w-2 h-2 rounded-full bg-amber-500"></span>
              <span class="w-2 h-2 rounded-full bg-emerald-500"></span>
              <span class="ml-2 text-[10px] text-gray-600">vps-provision.sh</span>
            </div>

            <!-- Terminal Output -->
            <div class="space-y-1 text-[10px] sm:text-[11px] leading-relaxed">
              <p class="text-blue-400">$ curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash</p>
              <p class="text-gray-500"># System: Ubuntu 24.04 LTS (x86_64)</p>
              <p class="text-gray-500"># Allocating server dependencies...</p>
              <p class="text-green-400">✔ Web Server (Nginx v1.24) installed</p>
              <p class="text-green-400">✔ PHP-FPM (Default PHP 8.4) installed</p>
              <p class="text-green-400">✔ Database (MariaDB v10.11) installed</p>
              <p class="text-green-400">✔ SSL Engine (Certbot Let's Encrypt) installed</p>
              <p class="text-green-400">✔ Security Wrapper (UFW Firewall) configured</p>
              <p class="text-blue-300">✔ Fluxo panel daemon initialized on port 9595!</p>
            </div>

            <div class="pt-4 border-t border-gray-900 flex flex-wrap gap-2 text-[10px]">
              <span class="px-2 py-0.5 rounded bg-gray-900 border border-gray-850 text-gray-300">
                Ubuntu 22.04 / 24.04
              </span>
              <span class="px-2 py-0.5 rounded bg-gray-900 border border-gray-850 text-gray-300">
                Debian 11 / 12
              </span>
              <span class="px-2 py-0.5 rounded bg-gray-900 border border-gray-850 text-gray-300">
                x86_64 / ARM64
              </span>
            </div>
          </div>
        </div>

        <!-- Text details -->
        <div class="space-y-6 text-left">
          <h2 class="text-3xl sm:text-4xl font-bold tracking-tight">
            Simple to Deploy,<br />
            <span class="text-blue-600 dark:text-blue-400">Compatible with Any VPS</span>
          </h2>
          <p class="text-gray-600 dark:text-gray-400 leading-relaxed text-sm">
            Fluxo runs directly on clean operating systems without container virtualization overhead, providing maximum
            performance and CGo-free efficiency. Set up your panel on any standard virtual instance in minutes.
          </p>

          <div class="space-y-6 pt-2">
            <div class="flex gap-4">
              <div
                class="w-10 h-10 rounded-lg bg-blue-50 dark:bg-blue-900/20 flex items-center justify-center shrink-0 text-blue-600 dark:text-blue-400 font-semibold text-lg">
                ⚡
              </div>
              <div>
                <h3 class="font-bold text-gray-900 dark:text-gray-100 text-base">One-Command Provisioning</h3>
                <p class="text-xs text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                  Our lightweight installer automatically configures Nginx, MariaDB, PHP runtimes, Certbot SSL
                  utilities, and systemd services, leaving you with a fully secured server right away.
                </p>
              </div>
            </div>

            <div class="flex gap-4">
              <div
                class="w-10 h-10 rounded-lg bg-blue-50 dark:bg-blue-900/20 flex items-center justify-center shrink-0 text-blue-600 dark:text-blue-400 font-semibold text-lg">
                ☁️
              </div>
              <div>
                <h3 class="font-bold text-gray-900 dark:text-gray-100 text-base">Run Anywhere</h3>
                <p class="text-xs text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                  Deploy on AWS, DigitalOcean, Hetzner Cloud, Google Cloud, Vultr, or your own dedicated bare metal
                  server. No provider lock-in, just pure Linux capability.
                </p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Runtime & Apps Section -->
    <section class="py-24 px-4 sm:px-6 lg:px-8 max-w-6xl mx-auto border-t border-gray-100 dark:border-gray-900">
      <div class="grid lg:grid-cols-2 gap-8 lg:gap-16 items-center">
        <!-- Text details -->
        <div class="space-y-6 lg:order-first order-last text-left">
          <h2 class="text-3xl sm:text-4xl font-bold tracking-tight">
            Isolated Environments for<br />
            <span class="text-blue-600 dark:text-blue-400">Multiple Applications</span>
          </h2>
          <p class="text-gray-600 dark:text-gray-400 leading-relaxed text-sm">
            Deploy different application types in isolated environments. Fluxo manages system user permissions, virtual
            host routing, and dedicated runtime processes for each site.
          </p>

          <div class="grid sm:grid-cols-2 gap-4 pt-2">
            <div class="p-5 rounded-xl border border-gray-150 dark:border-gray-900 bg-white dark:bg-gray-900/40">
              <div class="text-lg mb-2">🐘</div>
              <h4 class="font-bold text-sm text-gray-900 dark:text-gray-100">PHP Runtimes</h4>
              <p class="text-[11px] text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                Run Laravel, WordPress, or custom PHP code. Toggle PHP versions (7.4, 8.0, 8.1, 8.2, 8.3, or 8.4) with
                separate configuration profiles per application.
              </p>
            </div>

            <div class="p-5 rounded-xl border border-gray-150 dark:border-gray-900 bg-white dark:bg-gray-900/40">
              <div class="text-lg mb-2">🟢</div>
              <h4 class="font-bold text-sm text-gray-900 dark:text-gray-100">Node.js Services</h4>
              <p class="text-[11px] text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                Host custom JS application servers or frontend SSR builds. Install, remove, and restart processes with
                automatic Nginx proxy routing.
              </p>
            </div>

            <div class="p-5 rounded-xl border border-gray-150 dark:border-gray-900 bg-white dark:bg-gray-900/40">
              <div class="text-lg mb-2">🗄️</div>
              <h4 class="font-bold text-sm text-gray-900 dark:text-gray-100">Database Services</h4>
              <p class="text-[11px] text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                Start, stop, and restart database engines (MariaDB, PostgreSQL, or Redis cache) and manage credentials
                and user grants from the interface.
              </p>
            </div>

            <div class="p-5 rounded-xl border border-gray-150 dark:border-gray-900 bg-white dark:bg-gray-900/40">
              <div class="text-lg mb-2">📄</div>
              <h4 class="font-bold text-sm text-gray-900 dark:text-gray-100">Static Websites</h4>
              <p class="text-[11px] text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                Serve single-page frontend apps (Vue, React, Vite, or simple HTML/CSS/JS) with optimized virtual host
                configurations.
              </p>
            </div>
          </div>
        </div>

        <!-- Graphic (Site Creator Wizard mock) -->
        <div class="relative flex justify-center items-center">
          <div class="absolute -inset-4 bg-indigo-500/10 dark:bg-indigo-500/5 rounded-3xl blur-2xl"></div>

          <div
            class="relative w-full max-w-sm bg-white dark:bg-gray-900 rounded-2xl border border-gray-200 dark:border-gray-800 shadow-2xl p-6 space-y-4 text-left">
            <h4
              class="text-xs font-bold text-gray-900 dark:text-gray-100 flex items-center gap-1.5 pb-3 border-b border-gray-100 dark:border-gray-800">
              <span>✨</span> Create Site Wizard
            </h4>

            <div class="space-y-3">
              <div>
                <label class="block text-[9px] uppercase tracking-wider text-gray-400 font-bold mb-1">Application
                  Type</label>
                <div
                  class="w-full px-3 py-1.5 rounded-lg border border-gray-250 dark:border-gray-800 bg-gray-50 dark:bg-gray-950 text-xs text-gray-800 dark:text-gray-300 font-medium">
                  Laravel Application
                </div>
              </div>

              <div>
                <label class="block text-[9px] uppercase tracking-wider text-gray-400 font-bold mb-1">PHP
                  Version</label>
                <div
                  class="w-full px-3 py-1.5 rounded-lg border border-gray-255 dark:border-gray-800 bg-gray-55/50 dark:bg-gray-950 text-xs text-gray-800 dark:text-gray-300 font-medium flex justify-between items-center">
                  <span>PHP 8.4</span>
                  <span
                    class="text-[9px] bg-green-105 text-green-700 dark:bg-green-950/50 dark:text-green-400 px-1.5 py-0.5 rounded font-bold">Active</span>
                </div>
              </div>

              <div>
                <label class="block text-[9px] uppercase tracking-wider text-gray-400 font-bold mb-1">Database
                  Engine</label>
                <div
                  class="w-full px-3 py-1.5 rounded-lg border border-gray-255 dark:border-gray-800 bg-gray-55/50 dark:bg-gray-955 text-xs text-gray-800 dark:text-gray-300 font-medium">
                  MySQL / MariaDB
                </div>
              </div>

              <div class="pt-2 space-y-2">
                <div class="flex items-center gap-2">
                  <span
                    class="w-4 h-4 rounded-full bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center text-[10px] text-blue-600 dark:text-blue-400 font-bold">✓</span>
                  <span class="text-xs text-gray-600 dark:text-gray-400 text-left">Install Composer dependencies</span>
                </div>
                <div class="flex items-center gap-2">
                  <span
                    class="w-4 h-4 rounded-full bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center text-[10px] text-blue-600 dark:text-blue-400 font-bold">✓</span>
                  <span class="text-xs text-gray-600 dark:text-gray-400 text-left">Provision Let's Encrypt SSL</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Component Logos Grid -->
    <div class="py-12 bg-gray-50/50 dark:bg-gray-900/20 border-t border-b border-gray-100 dark:border-gray-900/60">
      <div class="max-w-6xl mx-auto px-4 text-center space-y-6">
        <p class="text-[9px] uppercase tracking-widest text-gray-400 dark:text-gray-500 font-bold">
          Configured Components & Integrations
        </p>
        <div
          class="flex flex-wrap items-center justify-center gap-6 md:gap-8 opacity-60 dark:opacity-50 grayscale hover:grayscale-0 transition-all duration-300">
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/nginx/009639" alt="NGINX"
              class="h-6" /><span class="font-bold text-sm tracking-tight">NGINX</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/mariadb/003545" alt="MariaDB"
              class="h-6" /><span class="font-bold text-sm tracking-tight">MariaDB</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/postgresql/4169E1" alt="PostgreSQL"
              class="h-6" /><span class="font-bold text-sm tracking-tight">PostgreSQL</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/php/777BB4" alt="PHP"
              class="h-6" /><span class="font-bold text-sm tracking-tight">PHP</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/redis/DC382D" alt="Redis"
              class="h-6" /><span class="font-bold text-sm tracking-tight">Redis</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/nodedotjs/5FA04E" alt="Node.js"
              class="h-6" /><span class="font-bold text-sm tracking-tight">Node.js</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/letsencrypt/003A70"
              alt="Let's Encrypt" class="h-6 dark:invert" /><span class="font-bold text-sm tracking-tight">Let's
              Encrypt</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/github/181717" alt="GitHub"
              class="h-6 dark:invert" /><span class="font-bold text-sm tracking-tight">GitHub</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/ubuntu/E95420" alt="Ubuntu Linux"
              class="h-6" /><span class="font-bold text-sm tracking-tight">Ubuntu</span></div>
        </div>
      </div>
    </div>

    <section id="demo" class="py-20 px-4 sm:px-6 lg:px-8 max-w-6xl mx-auto text-center">
      <h2 class="text-3xl sm:text-4xl font-bold tracking-tight mb-4">See it in action</h2>
      <p class="text-lg text-gray-600 dark:text-gray-400 max-w-2xl mx-auto mb-10">Try the live demo to explore the
        dashboard. No sign-up required — just click and browse.</p>
      <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
        class="inline-flex items-center gap-3 px-8 py-4 rounded-xl bg-blue-600 text-white font-semibold text-lg hover:bg-blue-700 transition-colors shadow-lg">
        <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        Launch Demo
      </a>
    </section>

    <section id="install"
      class="py-24 px-4 sm:px-6 lg:px-8 bg-gray-50/50 dark:bg-gray-900/10 border-t border-gray-100 dark:border-gray-900/60 backdrop-blur-3xl relative overflow-hidden">
      <!-- Glow background -->
      <div
        class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[500px] h-[500px] bg-blue-500/5 dark:bg-blue-500/2 rounded-full blur-3xl pointer-events-none">
      </div>

      <div class="max-w-6xl mx-auto grid lg:grid-cols-12 gap-8 lg:gap-12 items-center relative z-10">
        <!-- Text Column -->
        <div class="lg:col-span-5 space-y-6 text-left">
          <h2 class="text-3xl sm:text-4xl font-extrabold tracking-tight">
            Provision Your <span class="text-blue-600 dark:text-blue-400">Server</span>
          </h2>
          <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed font-sans">
            Deploy Fluxo directly to a clean server instance. The installer handles Nginx, PHP, MariaDB, Certbot SSL,
            and UFW firewall rules natively.
          </p>

          <div class="space-y-4 pt-2">
            <div class="flex items-center gap-3">
              <span
                class="w-6 h-6 rounded-full bg-blue-50 dark:bg-blue-900/20 flex items-center justify-center text-xs text-blue-650 dark:text-blue-400">🐧</span>
              <span class="text-xs font-semibold text-gray-700 dark:text-gray-300">Ubuntu 22.04+ or Debian 12+</span>
            </div>
            <div class="flex items-center gap-3">
              <span
                class="w-6 h-6 rounded-full bg-blue-50 dark:bg-blue-900/20 flex items-center justify-center text-xs text-blue-655 dark:text-blue-400">⚙️</span>
              <span class="text-xs font-semibold text-gray-700 dark:text-gray-300">Intel/AMD x86_64 or ARM64</span>
            </div>
            <div class="flex items-center gap-3">
              <span
                class="w-6 h-6 rounded-full bg-blue-50 dark:bg-blue-900/20 flex items-center justify-center text-xs text-blue-655 dark:text-blue-400">🔑</span>
              <span class="text-xs font-semibold text-gray-700 dark:text-gray-300">Requires Root SSH access</span>
            </div>
          </div>
        </div>

        <!-- Terminal Tabbed Column -->
        <div class="lg:col-span-7 w-full min-w-0">
          <div
            class="relative w-full bg-gray-950 dark:bg-black rounded-3xl border border-gray-200/60 dark:border-gray-850/60 shadow-2xl p-4 sm:p-6 space-y-4 overflow-hidden">

            <!-- Window header & Tabs -->
            <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-3 border-b border-gray-900">
              <div class="flex items-center gap-1.5">
                <span class="w-2.5 h-2.5 rounded-full bg-rose-500"></span>
                <span class="w-2.5 h-2.5 rounded-full bg-amber-500"></span>
                <span class="w-2.5 h-2.5 rounded-full bg-emerald-500"></span>
                <span class="ml-2 text-[10px] font-mono text-gray-500">fluxo-setup</span>
              </div>

              <!-- Tab Selectors -->
              <div
                class="flex bg-gray-900 p-0.5 rounded-lg border border-gray-850 text-[10px] font-semibold text-gray-400 self-start sm:self-auto">
                <button @click="activeTab = 'install'" class="px-2.5 py-1 rounded transition-colors cursor-pointer"
                  :class="activeTab === 'install' ? 'bg-gray-850 text-white font-bold' : 'hover:text-gray-200'">
                  1. Install
                </button>
                <button @click="activeTab = 'login'" class="px-2.5 py-1 rounded transition-colors cursor-pointer"
                  :class="activeTab === 'login' ? 'bg-gray-850 text-white font-bold' : 'hover:text-gray-200'">
                  2. Login
                </button>
                <button @click="activeTab = 'upgrade'" class="px-2.5 py-1 rounded transition-colors cursor-pointer"
                  :class="activeTab === 'upgrade' ? 'bg-gray-850 text-white font-bold' : 'hover:text-gray-200'">
                  3. Upgrade
                </button>
              </div>
            </div>

            <!-- Terminal Code Display -->
            <div class="relative group min-h-[140px] text-left">
              <pre v-if="activeTab === 'install'"
                class="text-[11px] sm:text-[12px] font-mono text-gray-300 leading-relaxed overflow-x-auto">
<span class="text-gray-500"># Fluxo version in this release: v{{ appVersion }}</span>
<span class="text-gray-500"># Run the installer command as root:</span>
<span class="text-blue-405 font-bold">curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash</span>

<span class="text-gray-500"># The installer automatically provisions:
# - Nginx Web Server & default PHP 8.4
# - UFW Firewall rules & Fail2Ban
# - Certbot Let's Encrypt engine
#
# It interactively prompts for Databases, Redis, and Node.js.
# For automated setups, you can bypass prompts using flags:
# curl ... | sudo bash -s -- --db-engine=mysql --redis --no-node
#
# Installed Fluxo version: v{{ appVersion }}</span></pre>

              <pre v-else-if="activeTab === 'login'"
                class="text-[11px] sm:text-[12px] font-mono text-gray-300 leading-relaxed overflow-x-auto">
<span class="text-gray-500"># 1. Open the Fluxo Web Panel in your browser:</span>
<span class="text-blue-405 font-bold">https://&lt;your-server-ip&gt;:9595</span>
<span class="text-gray-500"># (Ignore the self-signed certificate warning, click Advanced & Proceed)</span>

<span class="text-gray-500"># 2. Retrieve generated admin token credentials via SSH CLI:</span>
<span class="text-blue-405 font-bold">sudo cat /home/fluxo/.fluxo_credentials</span></pre>

              <pre v-else-if="activeTab === 'upgrade'"
                class="text-[11px] sm:text-[12px] font-mono text-gray-300 leading-relaxed overflow-x-auto">
<span class="text-gray-500"># Re-run the installer script to upgrade the binary:</span>
<span class="text-blue-405 font-bold">curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash</span>

<span class="text-gray-500"># Or pin to a specific stable release version:</span>
<span class="text-blue-405 font-bold">curl -fsSL https://fluxo.fottify.com/install.sh | FLUXO_VERSION=v0.2.0 sudo -E bash</span></pre>

              <!-- Floating Copy Button -->
              <button @click="copyInstall"
                class="absolute top-0 right-0 p-2 rounded-lg bg-gray-900 border border-gray-800 hover:bg-gray-850 hover:border-gray-700 text-gray-400 hover:text-gray-200 transition-all cursor-pointer"
                :title="copied ? 'Copied!' : 'Copy to Clipboard'">
                <svg v-if="!copied" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                    d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
                </svg>
                <span v-else class="text-[9px] font-bold text-green-500 block leading-none px-1">Copied!</span>
              </button>
            </div>

          </div>
        </div>
      </div>
    </section>

    <footer class="border-t border-gray-200 dark:border-gray-800 py-12 px-4 sm:px-6 lg:px-8">
      <div class="max-w-6xl mx-auto mb-8 text-center pb-8 border-b border-gray-100 dark:border-gray-850/60">
        <h3 class="font-bold text-gray-900 dark:text-gray-100 mb-2 flex items-center justify-center gap-2">
          <span>🤝</span> Community Contributions Welcomed
        </h3>
        <p class="text-sm text-gray-600 dark:text-gray-400 max-w-2xl mx-auto">
          Fluxo is Source-Available software. We actively welcome pull requests, bug reports, and ideas from the community. Help us shape the future of server management!
        </p>
      </div>

      <div class="max-w-6xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-4">
        <div class="flex items-center gap-2 font-bold text-lg">
          <img src="/logo.png" alt="fluxo" class="h-8 w-8 object-cover" />
          <span>fluxo</span>
        </div>
        <div class="flex items-center gap-6 text-sm text-gray-500 dark:text-gray-500">
          <a href="https://github.com/FabioTECH1/fluxo" target="_blank"
            class="hover:text-gray-700 dark:hover:text-gray-300 transition-colors">GitHub</a>
          <a href="https://github.com/FabioTECH1/fluxo/releases" target="_blank"
            class="hover:text-gray-700 dark:hover:text-gray-300 transition-colors">Releases</a>
          <a href="https://github.com/FabioTECH1/fluxo/issues" target="_blank"
            class="hover:text-gray-700 dark:hover:text-gray-300 transition-colors">Issues</a>
        </div>
        <p class="text-sm text-gray-400 dark:text-gray-600">Source-Available · BSL 1.1 License</p>
      </div>
    </footer>
  </div>
</template>
