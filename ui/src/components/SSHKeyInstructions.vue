<template>
  <section aria-label="How to get your public SSH key" class="space-y-3 rounded-lg border border-gray-200 bg-gray-50 p-4 text-sm text-gray-700 dark:border-gray-700 dark:bg-gray-800/50 dark:text-gray-300">
    <p>Run these commands in a terminal on <strong>your own computer</strong>, not on the server.</p>
    <ol class="space-y-4">
      <li v-for="(step, index) in steps" :key="step.command" class="space-y-2">
        <p class="font-semibold">{{ index + 1 }}. {{ step.title }}</p>
        <div class="flex flex-col gap-2 rounded-md border border-gray-200 bg-white p-2 dark:border-gray-600 dark:bg-gray-900 sm:flex-row sm:items-center">
          <code class="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-xs text-gray-900 dark:text-gray-100">{{ step.command }}</code>
          <AppButton type="button" variant="secondary" size="sm" class="shrink-0 self-end sm:self-auto" :aria-label="step.copyLabel" @click="copyCommand(step.command)">
            <ClipboardDocumentIcon class="h-4 w-4" aria-hidden="true" />
            {{ copiedCommand === step.command ? 'Copied!' : 'Copy' }}
          </AppButton>
        </div>
        <p class="text-xs leading-relaxed text-gray-600 dark:text-gray-400">{{ step.hint }}</p>
      </li>
    </ol>
    <p class="text-xs leading-relaxed">Paste the <strong>entire output line</strong>, starting with <code>ssh-ed25519</code>, into Public Key below. Only share the <code>.pub</code> file—never your private key.</p>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { ClipboardDocumentIcon } from '@heroicons/vue/24/outline';
import AppButton from './AppButton.vue';
import { useToast } from '../composables/useToast';

const steps = [
  {
    title: 'Generate a key (skip if you already have one)',
    command: 'ssh-keygen -t ed25519 -C "your@email.com"',
    copyLabel: 'Copy key generation command',
    hint: 'Replace your@email.com with your email. Press Enter to accept the default file location, then choose a passphrase. If a key already exists, do not overwrite it; use your existing public key instead.',
  },
  {
    title: 'Display your public key',
    command: 'cat ~/.ssh/id_ed25519.pub',
    copyLabel: 'Copy public key display command',
    hint: 'This prints your public key in the terminal. If you saved your key under a different filename, use that file’s .pub path instead.',
  },
];

const copiedCommand = ref('');
const { success, error } = useToast();
const copyCommand = async (command: string) => {
  copiedCommand.value = '';
  try {
    await navigator.clipboard.writeText(command);
    copiedCommand.value = command;
    success('Command copied. Paste it into your local terminal.');
  } catch {
    error('Could not copy the command. Select the command text and copy it manually.');
  }
};
</script>
