<template>
  <section v-if="updates.available.value" aria-label="Nginx package updates" class="mx-auto w-full max-w-6xl px-6 pt-3">
    <UpdateNotice :message="`Nginx ${snapshot?.candidate_version} is available from configured repositories.`" action-label="View instructions" dismiss-label="Dismiss this Nginx update notification" :dismissing="updates.busy.value" @action="updates.openInstructions" @dismiss="updates.dismiss" />
  </section>
  <BaseModal v-model="updates.instructionsOpen.value" title="Nginx update instructions" max-width="max-w-2xl">
    <p v-if="snapshot" class="break-all font-mono text-sm text-gray-700 dark:text-gray-300">{{ snapshot.package }} · {{ snapshot.installed_version }} → {{ snapshot.candidate_version || 'Not confirmed' }}</p>
    <p class="mt-3 text-sm leading-6 text-gray-700 dark:text-gray-300">Run these commands over SSH. Review the simulation before installing.</p>
    <p v-if="updates.state.value?.stale" class="mt-3 text-sm text-amber-700 dark:text-amber-300">Result is outdated. Check for updates again.</p>
    <p v-if="snapshot?.held" class="mt-3 text-sm text-amber-700 dark:text-amber-300">This package is held. Review the hold first.</p>
    <pre v-if="commands" class="mt-4 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-gray-950 p-4 text-xs leading-6 text-gray-100">{{ commands }}</pre>
    <p v-else class="mt-4 text-sm text-gray-600 dark:text-gray-400">Review this Nginx installation over SSH.</p>
    <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-gray-400">Updating Nginx may briefly interrupt sites.</p>
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
