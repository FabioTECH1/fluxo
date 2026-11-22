<template>
  <section v-if="updates.available.value" aria-label="Nginx package updates" class="mx-auto w-full max-w-6xl px-6 pt-3">
    <UpdateNotice :message="`Nginx ${snapshot?.candidate_version} is available from configured repositories.`" action-label="View instructions" dismiss-label="Dismiss this Nginx update notification" :dismissing="updates.busy.value" @action="updates.openInstructions" @dismiss="updates.dismiss" />
  </section>
  <BaseModal v-model="updates.instructionsOpen.value" title="Nginx update instructions" max-width="max-w-2xl">
    <p class="text-sm leading-6 text-gray-700 dark:text-gray-300">Fluxo checks package availability only. Run any package changes yourself over SSH after reviewing your configured repositories, package holds and the simulated transaction. Do not remove modules or change repositories just to match an upstream version.</p>
    <p v-if="updates.state.value?.stale" class="mt-3 text-sm text-amber-700 dark:text-amber-300">This result is stale. Check again and verify the current candidate before using these instructions.</p>
    <dl v-if="snapshot" class="mt-4 space-y-2 text-sm text-gray-700 dark:text-gray-300">
      <div><dt class="inline font-medium">Installed package: </dt><dd class="inline font-mono break-all">{{ snapshot.package }} {{ snapshot.installed_version }}</dd></div>
      <div><dt class="inline font-medium">Candidate: </dt><dd class="inline font-mono break-all">{{ snapshot.candidate_version || 'Not confirmed' }}</dd></div>
      <div><dt class="inline font-medium">Candidate repositories: </dt><dd class="inline">{{ snapshot.sources.map(s => [s.origin, s.archive, s.site].filter(Boolean).join(' · ')).join('; ') || 'Not confirmed' }}</dd></div>
    </dl>
    <p class="mt-4 text-sm leading-6 text-gray-700 dark:text-gray-300">APT may restart services during installation. Review the complete simulation and configuration changes first. Activating a new Nginx executable normally requires a real process restart, which can interrupt connections. The panel's <strong>Reload Nginx</strong> action reloads configuration; it does not prove adoption of a new binary.</p>
    <pre v-if="commands" class="mt-4 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-gray-950 p-4 text-xs leading-6 text-gray-100">{{ commands }}</pre>
    <p v-else class="mt-4 text-sm text-gray-600 dark:text-gray-400">No safe package-specific upgrade command is available for this result. Inspect the installation and APT policy over SSH.</p>
    <p class="mt-4 text-xs leading-5 text-gray-500 dark:text-gray-400">Running master/worker adoption is not verified. Check the service and hosted sites after your SSH work, then run Check for updates in Fluxo. Package installation does not itself mean the server requires a reboot; the separate server-restart notice reflects Ubuntu's actual state.</p>
    <template #footer><AppButton variant="secondary" @click="updates.instructionsOpen.value = false">Close</AppButton></template>
  </BaseModal>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue';
import { useNginxUpdates, nginxSSHCommands } from '../composables/useNginxUpdates';
import UpdateNotice from './UpdateNotice.vue';
import BaseModal from './BaseModal.vue';
import AppButton from './AppButton.vue';
const updates = useNginxUpdates();
const snapshot = computed(() => updates.state.value?.snapshot || null);
const commands = computed(() => nginxSSHCommands(snapshot.value));
onMounted(updates.start); onUnmounted(updates.stop);
</script>
