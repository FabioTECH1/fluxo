<template>
  <BaseModal v-model="visible" title="Rotate Database Password" :loading="loading" @submit="formRef?.requestSubmit()">
    <template #footer>
      <AppButton variant="secondary" @click="visible = false">Cancel</AppButton>
      <AppButton variant="primary" type="submit" :loading="loading" :disabled="loading" @click="formRef?.requestSubmit()">
        {{ loading ? 'Rotating...' : 'Rotate Password' }}
      </AppButton>
    </template>

    <form ref="formRef" class="space-y-5" @submit.prevent="submit">
      <ErrorAlert :message="error" />

      <div class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        This changes the password for <strong class="font-mono">{{ userName }}</strong> in {{ engineLabel }} only.
        Fluxo will not change any site environment file.
        <span v-if="affectedDatabases.length > 0">
          Update the application credentials for {{ affectedDatabases.join(', ') }} to avoid a database outage.
        </span>
      </div>

      <FormGroup label="New password" for-attr="rotated-database-password" hint="Use this same password when you manually update each affected application's environment file.">
        <PasswordInput id="rotated-database-password" v-model="password" :minlength="8" required placeholder="At least 8 characters" />
      </FormGroup>
    </form>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import BaseModal from '../components/BaseModal.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import FormGroup from '../components/FormGroup.vue';
import PasswordInput from '../components/PasswordInput.vue';

const props = defineProps<{
  userName: string;
  userEngine: string;
  affectedDatabases: string[];
}>();

const visible = defineModel<boolean>({ required: true });
const emit = defineEmits<{ rotated: [] }>();
const formRef = ref<HTMLFormElement | null>(null);
const password = ref('');
const loading = ref(false);
const error = ref('');

const engineLabel = computed(() => props.userEngine === 'postgres' ? 'PostgreSQL' : 'MySQL');

watch(visible, isVisible => {
  if (!isVisible) return;
  password.value = '';
  error.value = '';
});

const submit = async () => {
  loading.value = true;
  error.value = '';
  try {
    await apiClient.rotateDatabaseUserPassword({
      user: props.userName,
      password: password.value,
      engine: props.userEngine,
    });
    emit('rotated');
  } catch (e: any) {
    error.value = e.message || 'Failed to rotate database password';
  } finally {
    loading.value = false;
  }
};
</script>
