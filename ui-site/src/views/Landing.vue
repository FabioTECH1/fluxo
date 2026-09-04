<script setup lang="ts">
import { ref } from 'vue'
import {
  Activity,
  ArrowRight,
  ArrowUpRight,
  Check,
  Cloud,
  Code2,
  Cpu,
  DatabaseBackup,
  Globe2,
  HeartHandshake,
  KeyRound,
  Layers3,
  Menu,
  Monitor,
  Moon,
  PlayCircle,
  Rocket,
  Server,
  ShieldCheck,
  SquareTerminal,
  Sun,
  Workflow,
  X,
  Zap,
} from '@lucide/vue'
import { useTheme } from '@fluxo/composables/useTheme'
import { version as appVersion } from '../../package.json'
import CopyCommand from '../components/CopyCommand.vue'

const { theme } = useTheme()
const mobileMenuOpen = ref(false)
const activeTab = ref<'install' | 'login' | 'upgrade' | 'recovery'>('install')
const installCommand = 'curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash'
const automatedInstallCommand = 'curl -fsSL https://fluxo.fottify.com/install.sh | sudo bash -s -- --db-engine=mysql --redis --no-node'
const loginUrl = 'https://<your-server-ip>:9595'
const pinnedUpgradeCommand = `curl -fsSL https://fluxo.fottify.com/install.sh | FLUXO_VERSION=v${appVersion} sudo -E bash`
const showAdminUsernameCommand = 'sudo fluxo --show-admin-username'
const resetTokenCommand = 'sudo fluxo --reset-token'

function toggleTheme() {
  if (theme.value === 'dark') {
    theme.value = 'light'
  } else if (theme.value === 'light') {
    theme.value = 'system'
  } else {
    theme.value = 'dark'
  }
}

function currentTheme() {
  return theme.value
}

function scrollTo(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth' })
  mobileMenuOpen.value = false
}

</script>

<template>
  <div class="bg-white dark:bg-gray-950 text-gray-900 dark:text-gray-100 antialiased transition-colors"
    style="font-family: Inter, system-ui, sans-serif">
    <header
      class="fixed top-0 inset-x-0 z-50 border-b border-gray-200 dark:border-gray-800 bg-white/80 dark:bg-gray-950/80 backdrop-blur-md">
      <div class="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 h-16 flex items-center justify-between">
        <a href="/" class="flex items-center gap-2 font-bold text-xl">
          <img src="/logo.png" alt="fluxo" class="h-8 w-8 object-cover" />
          <span>fluxo</span>
        </a>
        <nav class="hidden md:flex items-center gap-8 text-sm font-medium text-gray-600 dark:text-gray-400">
          <button @click="scrollTo('features')"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors">Features</button>
          <a href="/docs/" target="_blank" rel="noopener noreferrer"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors">Documentation</a>
          <a href="/blog"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors">Blog</a>
          <button @click="scrollTo('install')"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors">Install</button>
          <a href="https://github.com/FabioTECH1/fluxo" target="_blank"
            class="hover:text-gray-900 dark:hover:text-gray-100 transition-colors">GitHub</a>
          <button @click="toggleTheme"
            class="flex h-9 w-9 items-center justify-center rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
            :aria-label="`Cycle color theme. Current theme: ${currentTheme()}`"
            :title="'Theme: ' + currentTheme()">
            <Sun v-if="currentTheme() === 'light'" class="h-4 w-4" aria-hidden="true" />
            <Moon v-else-if="currentTheme() === 'dark'" class="h-4 w-4" aria-hidden="true" />
            <Monitor v-else class="h-4 w-4" aria-hidden="true" />
          </button>
          <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
            class="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-blue-600 text-white font-semibold text-sm hover:bg-blue-700 transition-colors shadow-sm">
            Live Demo <ArrowRight class="h-4 w-4" aria-hidden="true" />
          </a>
        </nav>
        <div class="flex items-center gap-1 md:hidden">
          <button @click="toggleTheme"
            class="flex h-10 w-10 items-center justify-center rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
            :aria-label="`Cycle color theme. Current theme: ${currentTheme()}`"
            :title="'Theme: ' + currentTheme()">
            <Sun v-if="currentTheme() === 'light'" class="h-5 w-5" aria-hidden="true" />
            <Moon v-else-if="currentTheme() === 'dark'" class="h-5 w-5" aria-hidden="true" />
            <Monitor v-else class="h-5 w-5" aria-hidden="true" />
          </button>
          <button @click="mobileMenuOpen = !mobileMenuOpen"
            class="flex h-10 w-10 items-center justify-center rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
            :aria-expanded="mobileMenuOpen"
            aria-label="Toggle navigation menu">
            <X v-if="mobileMenuOpen" class="h-6 w-6" aria-hidden="true" />
            <Menu v-else class="h-6 w-6" aria-hidden="true" />
          </button>
        </div>
      </div>
      <div v-if="mobileMenuOpen"
        class="md:hidden border-t border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950 px-4 py-4 space-y-4 text-sm">
        <button @click="scrollTo('features')"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">Features</button>
        <a href="/docs/" target="_blank" rel="noopener noreferrer"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">Documentation</a>
        <a href="/blog"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">Blog</a>
        <button @click="scrollTo('install')"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">Install</button>
        <a href="https://github.com/FabioTECH1/fluxo" target="_blank"
          class="block w-full text-left py-2 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100">GitHub</a>

        <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
          class="flex w-full items-center justify-center gap-2 px-4 py-2.5 rounded-lg bg-blue-600 text-white font-semibold">
          Live Demo <ArrowRight class="h-4 w-4" aria-hidden="true" />
        </a>
      </div>
    </header>

    <section class="pt-28 pb-12 px-4 sm:px-6 lg:px-8 max-w-6xl mx-auto text-center sm:pt-32 sm:pb-16">
      <div
        class="inline-flex items-center gap-2 px-4 py-1.5 rounded-full bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 text-sm font-medium mb-6 border border-blue-200 dark:border-blue-800">
        <Rocket class="h-4 w-4" aria-hidden="true" />
        v{{ appVersion }} - Source-Available
      </div>
      <h1 class="text-4xl sm:text-5xl lg:text-6xl font-extrabold leading-tight">
        The self-hosted control panel
        <span class="block text-blue-600 dark:text-blue-400">for modern web apps</span>
      </h1>
      <p class="mt-6 text-lg sm:text-xl text-gray-600 dark:text-gray-400 max-w-2xl mx-auto leading-relaxed">
        Deploy Laravel, WordPress, PHP, Next.js, Nuxt, Node.js, and static sites on your own VPS. Fluxo manages Nginx,
        SSL, databases, deployments, processes, and backups from one dashboard.
      </p>
      <div class="mt-8 flex flex-col sm:flex-row items-center justify-center gap-4">
        <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
          class="w-full sm:w-auto inline-flex items-center justify-center gap-2 px-8 py-3.5 rounded-lg bg-blue-600 text-white font-semibold text-lg hover:bg-blue-700 transition-colors shadow-lg shadow-blue-600/20 dark:shadow-blue-600/10">
          <PlayCircle class="h-5 w-5" aria-hidden="true" />
          Live Demo
        </a>
        <button @click="scrollTo('install')"
          class="w-full sm:w-auto inline-flex items-center justify-center gap-2 px-8 py-3.5 rounded-lg border border-gray-300 dark:border-gray-700 text-gray-700 dark:text-gray-300 font-semibold text-lg hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
          <SquareTerminal class="h-5 w-5" aria-hidden="true" />
          Install Now
        </button>
      </div>
      <a href="/docs/" target="_blank" rel="noopener noreferrer"
        class="mt-5 inline-flex items-center gap-1.5 text-sm font-semibold text-gray-600 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400">
        Read the documentation <ArrowUpRight class="h-4 w-4" aria-hidden="true" />
      </a>
    </section>

    <section class="px-4 pb-16 sm:px-6 sm:pb-20 lg:px-8">
      <div class="mx-auto max-w-6xl">
        <div class="mb-5 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <p class="text-xs font-semibold uppercase text-blue-600 dark:text-blue-400">The actual control panel</p>
            <h2 class="mt-1 text-2xl font-bold">See what you will manage</h2>
          </div>
          <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
            class="inline-flex items-center gap-1.5 text-sm font-semibold text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300">
            Explore the live demo <ArrowRight class="h-4 w-4" aria-hidden="true" />
          </a>
        </div>
        <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
          class="block overflow-hidden rounded-lg border border-gray-200 bg-gray-950 shadow-xl dark:border-gray-800"
          aria-label="Open the Fluxo live demo">
          <img src="/og-image.png"
            alt="Fluxo sites dashboard showing Laravel, PHP, WordPress, Next.js, and static sites"
            class="block h-auto w-full" />
        </a>
      </div>
    </section>

    <section id="features" class="py-16 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-900/50 sm:py-20">
      <div class="max-w-6xl mx-auto">
        <div class="text-center mb-12">
          <h2 class="text-3xl sm:text-4xl font-bold">Run your apps and server from one place</h2>
          <p class="mt-4 text-lg text-gray-600 dark:text-gray-400 max-w-2xl mx-auto">No hosted control plane and no
            recurring panel fee. Fluxo runs on supported Ubuntu servers under your control.</p>
        </div>
        <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-6">
          <div
            class="p-6 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-blue-100 dark:bg-blue-900/40 flex items-center justify-center text-blue-600 dark:text-blue-400 text-lg mb-4">
              <Layers3 class="h-5 w-5" aria-hidden="true" />
            </div>
            <h3 class="font-semibold text-lg mb-2">Multiple Application Types</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Deploy Laravel, WordPress, custom PHP,
              Next.js, Nuxt, Node.js, and static sites with app-aware defaults.</p>
          </div>
          <div
            class="p-6 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-emerald-100 dark:bg-emerald-900/40 flex items-center justify-center text-emerald-600 dark:text-emerald-400 text-lg mb-4">
              <Workflow class="h-5 w-5" aria-hidden="true" />
            </div>
            <h3 class="font-semibold text-lg mb-2">Release-Based Deployments</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Build away from live traffic, activate
              completed releases atomically, trigger from GitHub, and keep failures visible with full output.</p>
          </div>
          <div
            class="p-6 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-green-100 dark:bg-green-900/40 flex items-center justify-center text-green-600 dark:text-green-400 text-lg mb-4">
              <ShieldCheck class="h-5 w-5" aria-hidden="true" />
            </div>
            <h3 class="font-semibold text-lg mb-2">Domains and Certificates</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Manage application and panel domains,
              Let's Encrypt, custom certificates, and compatible wildcard certificate cloning from the dashboard.</p>
          </div>
          <div
            class="p-6 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-purple-100 dark:bg-purple-900/40 flex items-center justify-center text-purple-600 dark:text-purple-400 text-lg mb-4">
              <DatabaseBackup class="h-5 w-5" aria-hidden="true" />
            </div>
            <h3 class="font-semibold text-lg mb-2">Databases and Backups</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Manage MySQL, MariaDB, and PostgreSQL,
              then back up site files and databases to private S3 or R2 storage.</p>
          </div>
          <div
            class="p-6 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-orange-100 dark:bg-orange-900/40 flex items-center justify-center text-orange-600 dark:text-orange-400 text-lg mb-4">
              <Activity class="h-5 w-5" aria-hidden="true" />
            </div>
            <h3 class="font-semibold text-lg mb-2">Processes and Automation</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Supervise Node and Laravel processes,
              schedulers, queues, cron jobs, and other systemd daemons.</p>
          </div>
          <div
            class="p-6 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 hover:shadow-lg transition-shadow">
            <div
              class="w-10 h-10 rounded-lg bg-rose-100 dark:bg-rose-900/40 flex items-center justify-center text-rose-600 dark:text-rose-400 text-lg mb-4">
              <SquareTerminal class="h-5 w-5" aria-hidden="true" />
            </div>
            <h3 class="font-semibold text-lg mb-2">Operations and Diagnostics</h3>
            <p class="text-gray-600 dark:text-gray-400 text-sm leading-relaxed">Run commands, inspect deployment output,
              tail logs, monitor resources, manage SSH keys, and configure UFW.</p>
          </div>
        </div>
      </div>
    </section>

    <!-- Provisioning Section -->
    <section class="py-20 px-4 sm:px-6 lg:px-8 max-w-6xl mx-auto border-t border-gray-100 dark:border-gray-900">
      <div class="grid lg:grid-cols-2 gap-8 lg:gap-16 items-center">
        <!-- Graphic -->
        <div class="flex justify-center items-center">
          <div
            class="w-full max-w-md bg-gray-950 dark:bg-black rounded-lg border border-gray-800 shadow-xl p-4 sm:p-6 font-mono text-xs text-gray-400 space-y-4 text-left">
            <!-- Window header -->
            <div class="flex items-center gap-1.5 pb-2 border-b border-gray-900">
              <span class="w-2 h-2 rounded-full bg-rose-500"></span>
              <span class="w-2 h-2 rounded-full bg-amber-500"></span>
              <span class="w-2 h-2 rounded-full bg-emerald-500"></span>
              <span class="ml-2 text-xs text-gray-500">vps-provision.sh</span>
            </div>

            <!-- Terminal Output -->
            <div class="space-y-1 text-xs leading-relaxed">
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

            <div class="pt-4 border-t border-gray-900 flex flex-wrap gap-2 text-xs">
              <span class="px-2 py-0.5 rounded bg-gray-900 border border-gray-850 text-gray-300">
                Ubuntu 22.04+
              </span>
              <span class="px-2 py-0.5 rounded bg-gray-900 border border-gray-850 text-gray-300">
                x86_64 / ARM64
              </span>
            </div>
          </div>
        </div>

        <!-- Text details -->
        <div class="space-y-6 text-left">
          <h2 class="text-3xl sm:text-4xl font-bold">
            Deploy on your<br />
            <span class="text-blue-600 dark:text-blue-400">preferred VPS</span>
          </h2>
          <p class="text-gray-600 dark:text-gray-400 leading-relaxed text-base">
            Fluxo runs directly on a clean supported Ubuntu server. The installer validates the host and release-pinned tools before configuring services natively,
            without requiring a hosted control plane or container runtime.
          </p>

          <div class="space-y-6 pt-2">
            <div class="flex gap-4">
              <div
                class="w-10 h-10 rounded-lg bg-blue-50 dark:bg-blue-900/20 flex items-center justify-center shrink-0 text-blue-600 dark:text-blue-400 font-semibold text-lg">
                <Zap class="h-5 w-5" aria-hidden="true" />
              </div>
              <div>
                <h3 class="font-bold text-gray-900 dark:text-gray-100 text-base">One-Command Provisioning</h3>
                <p class="text-sm text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                  Select the database, Redis, and Node.js options you need. Fluxo configures Nginx, PHP, Certbot, UFW,
                  and systemd services around those choices.
                </p>
              </div>
            </div>

            <div class="flex gap-4">
              <div
                class="w-10 h-10 rounded-lg bg-blue-50 dark:bg-blue-900/20 flex items-center justify-center shrink-0 text-blue-600 dark:text-blue-400 font-semibold text-lg">
                <Cloud class="h-5 w-5" aria-hidden="true" />
              </div>
              <div>
                <h3 class="font-bold text-gray-900 dark:text-gray-100 text-base">Run Anywhere</h3>
                <p class="text-sm text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                  Deploy on AWS, DigitalOcean, Hetzner Cloud, Google Cloud, Vultr, or your own dedicated bare metal
                  server using a supported operating-system image. Your applications remain on infrastructure you control.
                </p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Runtime & Apps Section -->
    <section class="py-20 px-4 sm:px-6 lg:px-8 max-w-6xl mx-auto border-t border-gray-100 dark:border-gray-900">
      <div class="grid lg:grid-cols-2 gap-8 lg:gap-16 items-center">
        <!-- Text details -->
        <div class="space-y-6 text-left">
          <h2 class="text-3xl sm:text-4xl font-bold">
            One server, multiple<br />
            <span class="text-blue-600 dark:text-blue-400">application stacks</span>
          </h2>
          <p class="text-gray-600 dark:text-gray-400 leading-relaxed text-base">
            Run PHP, Node.js, static sites, databases, and managed processes from one control panel. Fluxo applies the
            appropriate Nginx and runtime configuration for each site type.
          </p>

          <div class="grid sm:grid-cols-2 gap-4 pt-2">
            <div class="p-5 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900/40">
              <Code2 class="mb-2 h-5 w-5 text-indigo-600 dark:text-indigo-400" aria-hidden="true" />
              <h4 class="font-bold text-sm text-gray-900 dark:text-gray-100">PHP Runtimes</h4>
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                Run Laravel, WordPress, or custom PHP code. Toggle PHP versions and enable Laravel-focused helpers like
                Scheduler, Queue Workers, Nightwatch, Horizon, maintenance mode, and Octane where it fits.
              </p>
            </div>

            <div class="p-5 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900/40">
              <Server class="mb-2 h-5 w-5 text-emerald-600 dark:text-emerald-400" aria-hidden="true" />
              <h4 class="font-bold text-sm text-gray-900 dark:text-gray-100">Node.js Services</h4>
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                Host Next.js, Nuxt, or custom JavaScript and TypeScript servers. Choose npm, pnpm, Yarn, or Bun, then let
                Fluxo build and supervise the app process.
              </p>
            </div>

            <div class="p-5 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900/40">
              <DatabaseBackup class="mb-2 h-5 w-5 text-purple-600 dark:text-purple-400" aria-hidden="true" />
              <h4 class="font-bold text-sm text-gray-900 dark:text-gray-100">Database Services</h4>
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                Start, stop, and restart database engines (MariaDB, PostgreSQL, or Redis cache) and manage credentials
                and user grants from the interface.
              </p>
            </div>

            <div class="p-5 rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900/40">
              <Globe2 class="mb-2 h-5 w-5 text-orange-600 dark:text-orange-400" aria-hidden="true" />
              <h4 class="font-bold text-sm text-gray-900 dark:text-gray-100">Static Websites</h4>
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
                Serve single-page frontend apps (Vue, React, Vite, or simple HTML/CSS/JS) with optimized virtual host
                configurations and release-based deployments when connected to Git.
              </p>
            </div>
          </div>
        </div>

        <!-- Graphic (Site Creator Wizard mock) -->
        <div class="flex justify-center items-center">
          <div
            class="w-full max-w-sm bg-white dark:bg-gray-900 rounded-lg border border-gray-200 dark:border-gray-800 shadow-xl p-6 space-y-4 text-left">
            <h4
              class="text-xs font-bold text-gray-900 dark:text-gray-100 flex items-center gap-1.5 pb-3 border-b border-gray-100 dark:border-gray-800">
              <Layers3 class="h-4 w-4 text-blue-600 dark:text-blue-400" aria-hidden="true" /> Create Site Wizard
            </h4>

            <div class="space-y-3">
              <div>
                <label class="block text-xs uppercase text-gray-500 dark:text-gray-400 font-bold mb-1">Application
                  Type</label>
                <div class="grid grid-cols-2 gap-2">
                  <div
                    class="px-3 py-2 rounded-lg border border-blue-200 dark:border-blue-900/60 bg-blue-50 dark:bg-blue-900/20 text-xs text-blue-800 dark:text-blue-200 font-semibold">
                    Laravel
                  </div>
                  <div
                    class="px-3 py-2 rounded-lg border border-gray-250 dark:border-gray-800 bg-gray-50 dark:bg-gray-950 text-xs text-gray-600 dark:text-gray-400 font-medium">
                    Node.js
                  </div>
                </div>
              </div>

              <div>
                <label class="block text-xs uppercase text-gray-500 dark:text-gray-400 font-bold mb-1">PHP
                  Version</label>
                <div
                  class="w-full px-3 py-1.5 rounded-lg border border-gray-255 dark:border-gray-800 bg-gray-55/50 dark:bg-gray-950 text-xs text-gray-800 dark:text-gray-300 font-medium flex justify-between items-center">
                  <span>PHP 8.4</span>
                  <span
                    class="text-xs bg-green-100 text-green-700 dark:bg-green-950/50 dark:text-green-400 px-1.5 py-0.5 rounded font-bold">Active</span>
                </div>
              </div>

              <div>
                <label class="block text-xs uppercase text-gray-500 dark:text-gray-400 font-bold mb-1">Database
                  Engine</label>
                <div
                  class="w-full px-3 py-1.5 rounded-lg border border-gray-255 dark:border-gray-800 bg-gray-55/50 dark:bg-gray-955 text-xs text-gray-800 dark:text-gray-300 font-medium">
                  MySQL / MariaDB
                </div>
              </div>

              <div
                class="rounded-lg border border-emerald-200 dark:border-emerald-900/50 bg-emerald-50 dark:bg-emerald-900/20 px-3 py-2 text-xs text-emerald-800 dark:text-emerald-200">
                Next.js and Nuxt use the Node.js option with npm, pnpm, Yarn, or Bun builds and a managed daemon.
              </div>

              <div class="pt-2 space-y-2">
                <div class="flex items-center gap-2">
                  <span class="flex h-4 w-4 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400">
                    <Check class="h-3 w-3" aria-hidden="true" />
                  </span>
                  <span class="text-xs text-gray-600 dark:text-gray-400 text-left">Install Composer dependencies</span>
                </div>
                <div class="flex items-center gap-2">
                  <span class="flex h-4 w-4 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400">
                    <Check class="h-3 w-3" aria-hidden="true" />
                  </span>
                  <span class="text-xs text-gray-600 dark:text-gray-400 text-left">Provision Let's Encrypt SSL</span>
                </div>
                <div class="flex items-center gap-2">
                  <span class="flex h-4 w-4 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400">
                    <Check class="h-3 w-3" aria-hidden="true" />
                  </span>
                  <span class="text-xs text-gray-600 dark:text-gray-400 text-left">Activate the completed release</span>
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
        <p class="text-xs uppercase text-gray-500 dark:text-gray-400 font-bold">
          Configured Components & Integrations
        </p>
        <div
          class="flex flex-wrap items-center justify-center gap-6 md:gap-8 opacity-60 dark:opacity-50 grayscale hover:grayscale-0 transition-all duration-300">
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/nginx/009639" alt="NGINX"
              class="h-6" /><span class="font-bold text-sm">NGINX</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/mariadb/003545" alt="MariaDB"
              class="h-6" /><span class="font-bold text-sm">MariaDB</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/phpmyadmin/6C78AF" alt="phpMyAdmin"
              class="h-6" /><span class="font-bold text-sm">phpMyAdmin</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/postgresql/4169E1" alt="PostgreSQL"
              class="h-6" /><span class="font-bold text-sm">PostgreSQL</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/php/777BB4" alt="PHP"
              class="h-6" /><span class="font-bold text-sm">PHP</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/laravel/FF2D20" alt="Laravel"
              class="h-6" /><span class="font-bold text-sm">Laravel</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/wordpress/21759B" alt="WordPress"
              class="h-6" /><span class="font-bold text-sm">WordPress</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/redis/DC382D" alt="Redis"
              class="h-6" /><span class="font-bold text-sm">Redis</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/nodedotjs/5FA04E" alt="Node.js"
              class="h-6" /><span class="font-bold text-sm">Node.js</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/nextdotjs/000000" alt="Next.js"
              class="h-6 dark:invert" /><span class="font-bold text-sm">Next.js</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/nuxt/00DC82" alt="Nuxt"
              class="h-6" /><span class="font-bold text-sm">Nuxt</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/letsencrypt/003A70"
              alt="Let's Encrypt" class="h-6 dark:invert" /><span class="font-bold text-sm">Let's
              Encrypt</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/github/181717" alt="GitHub"
              class="h-6 dark:invert" /><span class="font-bold text-sm">GitHub</span></div>
          <div class="flex items-center gap-2"><img src="https://cdn.simpleicons.org/ubuntu/E95420" alt="Ubuntu Linux"
              class="h-6" /><span class="font-bold text-sm">Ubuntu</span></div>
        </div>
      </div>
    </div>

    <section id="demo" class="py-20 px-4 sm:px-6 lg:px-8 max-w-6xl mx-auto text-center">
      <h2 class="text-3xl sm:text-4xl font-bold mb-4">See it in action</h2>
      <p class="text-lg text-gray-600 dark:text-gray-400 max-w-2xl mx-auto mb-10">Try the live demo to explore the
        dashboard. No sign-up required. Open it and explore.</p>
      <a href="/demo/sites" target="_blank" rel="noopener noreferrer"
        class="inline-flex items-center gap-3 px-8 py-4 rounded-lg bg-blue-600 text-white font-semibold text-lg hover:bg-blue-700 transition-colors shadow-lg">
        <PlayCircle class="h-6 w-6" aria-hidden="true" />
        Launch Demo
      </a>
    </section>

    <section id="install"
      class="py-20 px-4 sm:px-6 lg:px-8 bg-gray-50/50 dark:bg-gray-900/10 border-t border-gray-100 dark:border-gray-900/60">
      <div class="max-w-6xl mx-auto grid lg:grid-cols-12 gap-8 lg:gap-12 items-center">
        <!-- Text Column -->
        <div class="lg:col-span-5 space-y-6 text-left">
          <h2 class="text-3xl sm:text-4xl font-extrabold">
            Provision Your <span class="text-blue-600 dark:text-blue-400">Server</span>
          </h2>
          <p class="text-gray-600 dark:text-gray-400 text-base leading-relaxed font-sans">
            Deploy Fluxo directly to a clean server instance. The installer handles Nginx, PHP, WP-CLI, optional
            Node.js,
            databases, Certbot SSL, and UFW firewall rules natively.
          </p>

          <div class="space-y-4 pt-2">
            <div class="flex items-center gap-3">
              <span class="flex h-7 w-7 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-900/20 dark:text-blue-400">
                <Server class="h-4 w-4" aria-hidden="true" />
              </span>
              <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">Ubuntu 22.04 or newer</span>
            </div>
            <div class="flex items-center gap-3">
              <span class="flex h-7 w-7 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-900/20 dark:text-blue-400">
                <Cpu class="h-4 w-4" aria-hidden="true" />
              </span>
              <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">Intel/AMD x86_64 or ARM64</span>
            </div>
            <div class="flex items-center gap-3">
              <span class="flex h-7 w-7 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-900/20 dark:text-blue-400">
                <KeyRound class="h-4 w-4" aria-hidden="true" />
              </span>
              <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">Requires root SSH access</span>
            </div>
          </div>
        </div>

        <!-- Terminal Tabbed Column -->
        <div class="lg:col-span-7 w-full min-w-0">
          <div
            class="w-full bg-gray-950 dark:bg-black rounded-lg border border-gray-800 shadow-xl p-4 sm:p-6 space-y-4 overflow-hidden">

            <!-- Window header & Tabs -->
            <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-3 border-b border-gray-900">
              <div class="flex items-center gap-1.5">
                <span class="w-2.5 h-2.5 rounded-full bg-rose-500"></span>
                <span class="w-2.5 h-2.5 rounded-full bg-amber-500"></span>
                <span class="w-2.5 h-2.5 rounded-full bg-emerald-500"></span>
                <span class="ml-2 text-xs font-mono text-gray-500">fluxo-setup</span>
              </div>

              <!-- Tab Selectors -->
              <div
                class="grid w-full grid-cols-2 bg-gray-900 p-0.5 rounded-lg border border-gray-800 text-xs font-semibold text-gray-400 sm:flex sm:w-auto self-start sm:self-auto">
                <button @click="activeTab = 'install'" class="px-2.5 py-1.5 rounded transition-colors cursor-pointer"
                  :class="activeTab === 'install' ? 'bg-gray-850 text-white font-bold' : 'hover:text-gray-200'">
                  1. Install
                </button>
                <button @click="activeTab = 'login'" class="px-2.5 py-1.5 rounded transition-colors cursor-pointer"
                  :class="activeTab === 'login' ? 'bg-gray-850 text-white font-bold' : 'hover:text-gray-200'">
                  2. Login
                </button>
                <button @click="activeTab = 'upgrade'" class="px-2.5 py-1.5 rounded transition-colors cursor-pointer"
                  :class="activeTab === 'upgrade' ? 'bg-gray-850 text-white font-bold' : 'hover:text-gray-200'">
                  3. Upgrade
                </button>
                <button @click="activeTab = 'recovery'" class="px-2.5 py-1.5 rounded transition-colors cursor-pointer"
                  :class="activeTab === 'recovery' ? 'bg-gray-850 text-white font-bold' : 'hover:text-gray-200'">
                  4. Account Recovery
                </button>
              </div>
            </div>

            <!-- Terminal Code Display -->
            <div class="min-h-[140px] text-left">
              <div v-if="activeTab === 'install'"
                class="space-y-3 font-mono text-xs leading-relaxed text-gray-300">
                <div class="space-y-1 text-gray-500">
                  <p># Fluxo version in this release: v{{ appVersion }}</p>
                  <p># Run the installer command as root:</p>
                </div>
                <CopyCommand :command="installCommand" />
                <div class="space-y-1 text-gray-500">
                  <p># The installer automatically provisions:</p>
                  <p># - Nginx Web Server &amp; default PHP 8.4</p>
                  <p># - Safe UFW defaults &amp; Fail2Ban</p>
                  <p># - Certbot Let's Encrypt engine</p>
                  <p>#</p>
                  <p># It interactively prompts for Databases, Redis, and the Node.js toolchain.</p>
                </div>
                <p class="text-gray-500"># For automated setups, you can bypass prompts using flags:</p>
                <CopyCommand :command="automatedInstallCommand" />
                <p class="text-gray-500"># Installed Fluxo version: v{{ appVersion }}</p>
              </div>

              <div v-else-if="activeTab === 'login'"
                class="space-y-3 font-mono text-xs leading-relaxed text-gray-300">
                <div class="space-y-1 text-gray-500">
                  <p># 1. Open the Fluxo Web Panel in your browser:</p>
                </div>
                <CopyCommand :command="loginUrl" />
                <p class="text-gray-500"># Ignore the self-signed certificate warning, then click Advanced &amp; Proceed.</p>
                <p class="text-gray-500"># 2. Use the bootstrap token displayed when provisioning completes.</p>
              </div>

              <div v-else-if="activeTab === 'upgrade'"
                class="space-y-3 font-mono text-xs leading-relaxed text-gray-300">
                <p class="text-gray-500"># Re-run the installer script to upgrade the binary:</p>
                <CopyCommand :command="installCommand" />
                <p class="text-gray-500"># Or pin to a specific stable release version:</p>
                <CopyCommand :command="pinnedUpgradeCommand" />
              </div>

              <div v-else class="space-y-3 font-mono text-xs leading-relaxed text-gray-300">
                <p class="text-gray-500"># Retrieve the admin username without changing the account:</p>
                <CopyCommand :command="showAdminUsernameCommand" />
                <p class="text-gray-500"># Generate a new admin login token if you are locked out:</p>
                <CopyCommand :command="resetTokenCommand" />
                <p class="text-gray-500"># Reset displays the username and new token. Existing sessions are invalidated.</p>
              </div>
            </div>

          </div>
        </div>
      </div>
    </section>

    <footer class="border-t border-gray-200 dark:border-gray-800 py-12 px-4 sm:px-6 lg:px-8">
      <div class="max-w-6xl mx-auto mb-8 text-center pb-8 border-b border-gray-100 dark:border-gray-850/60">
        <h3 class="font-bold text-gray-900 dark:text-gray-100 mb-2 flex items-center justify-center gap-2">
          <HeartHandshake class="h-5 w-5 text-blue-600 dark:text-blue-400" aria-hidden="true" />
          Community Contributions Welcome
        </h3>
        <p class="text-sm text-gray-600 dark:text-gray-400 max-w-2xl mx-auto">
          Fluxo is Source-Available software. We actively welcome pull requests, bug reports, and ideas from the
          community. Help shape the future of server management.
        </p>
      </div>

      <div class="max-w-6xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-4">
        <div class="flex items-center gap-2 font-bold text-lg">
          <img src="/logo.png" alt="fluxo" class="h-8 w-8 object-cover" />
          <span>fluxo</span>
        </div>
        <div class="flex items-center gap-6 text-sm text-gray-500 dark:text-gray-500">
          <a href="/docs/" target="_blank" rel="noopener noreferrer"
            class="hover:text-gray-700 dark:hover:text-gray-300 transition-colors">Documentation</a>
          <a href="/blog"
            class="hover:text-gray-700 dark:hover:text-gray-300 transition-colors">Blog</a>
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
