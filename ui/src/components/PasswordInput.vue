<template>
  <div class="relative">
    <input
      :id="id"
      v-model="password"
      :type="showPassword ? 'text' : 'password'"
      :required="required"
      :minlength="minlength"
      :maxlength="maxlength"
      :autocomplete="autocomplete"
      :disabled="disabled"
      class="w-full rounded-lg border border-gray-200 py-2 pl-3 pr-28 font-mono text-sm transition-shadow focus:border-blue-500 focus:ring-2 focus:ring-blue-500 disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
      :placeholder="placeholder"
    >
    <div class="absolute inset-y-0 right-0 flex items-center gap-1 pr-2">
      <button
        v-if="allowGenerate"
        type="button"
        :disabled="disabled"
        class="px-2 py-1 text-xs font-semibold text-blue-600 hover:text-blue-800 disabled:opacity-50 dark:text-blue-400 dark:hover:text-blue-300"
        @click="generatePassword"
      >
        Generate
      </button>
      <button
        type="button"
        :disabled="disabled"
        class="text-gray-400 hover:text-gray-600 disabled:opacity-50 dark:text-gray-500 dark:hover:text-gray-300"
        :aria-label="showPassword ? 'Hide password' : 'Show password'"
        @click="showPassword = !showPassword"
      >
        <span class="text-lg leading-none">{{ showPassword ? '🙈' : '👁' }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';

const props = withDefaults(defineProps<{
  id?: string;
  required?: boolean;
  minlength?: number;
  maxlength?: number;
  autocomplete?: string;
  placeholder?: string;
  disabled?: boolean;
  allowGenerate?: boolean;
  generatedLength?: number;
}>(), {
  id: undefined,
  required: false,
  minlength: 8,
  maxlength: 256,
  autocomplete: 'new-password',
  placeholder: 'Enter a password or click Generate',
  disabled: false,
  allowGenerate: true,
  generatedLength: 20,
});

const password = defineModel<string>({ required: true });
const showPassword = ref(false);

const generatePassword = () => {
  const characters = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
  const limit = 256 - (256 % characters.length);
  const length = Math.min(props.maxlength, Math.max(props.minlength, props.generatedLength));
  let generated = '';
  while (generated.length < length) {
    const random = new Uint8Array(32);
    crypto.getRandomValues(random);
    for (const value of random) {
      if (value < limit) generated += characters[value % characters.length];
      if (generated.length === length) break;
    }
  }
  password.value = generated;
  showPassword.value = true;
};

watch(password, value => {
  if (!value) showPassword.value = false;
});
</script>
