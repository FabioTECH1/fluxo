<template>
  <div class="space-y-5">
    <PageHeader title="Tinker" subtitle="An interactive PHP session in your running Laravel application." />
    <ErrorAlert v-if="error && !showPassword" :message="error" />
    <Card>
      <div class="space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div><p class="font-medium">{{ site?.domain || 'Laravel application' }}</p><p class="text-sm text-gray-500">Live application · PHP {{ site?.php_version }} · runs as fluxo</p></div>
          <StatusBadge :label="status" :variant="connected ? 'green' : 'gray'" />
        </div>
        <p class="text-sm text-gray-500">Code runs against live data and can change files, secrets and databases accessible to the shared fluxo account. Closing the session does not undo changes.</p>
        <p class="text-sm text-gray-500">Variables persist while connected. Sessions end after 5 minutes without input or 15 minutes total. Fluxo does not record console input or output.</p>
        <div class="flex gap-3">
          <AppButton v-if="!socket && !connecting" :disabled="site?.app_type !== 'laravel'" @click="openConnect">Connect</AppButton>
          <template v-else><AppButton variant="secondary" @click="reset">Reset session</AppButton><AppButton variant="danger" @click="disconnect">Disconnect</AppButton></template>
        </div>
        <p v-if="site && site.app_type !== 'laravel'" class="text-sm text-gray-500">Tinker is available only for Laravel sites.</p>
        <p v-if="ended" class="text-sm text-gray-500" role="status">{{ ended }} Reconnect to start a fresh session.</p>
      </div>
    </Card>
    <InteractiveTerminal v-if="socket" ref="terminal" @input="sendInput" @resize="sendResize" @overflow="outputOverflow" @rendered="acknowledgeOutput" />
    <BaseModal v-model="showPassword" title="Connect to live Tinker" :loading="connecting">
      <form id="connect-tinker" class="space-y-4" @submit.prevent="connect">
 <ErrorAlert v-if="error" :message="error" />
        <FormGroup label="Your Fluxo password" forAttr="tinker-password" hint="Verify your password for each new session. Laravel Tinker must be installed in this application's production dependencies.">
          <input id="tinker-password" v-model="password" type="password" autocomplete="current-password" required maxlength="1024" class="w-full rounded-lg border border-gray-300 bg-transparent px-3 py-2" />
        </FormGroup>
      </form>
      <template #footer><AppButton form="connect-tinker" type="submit" :loading="connecting">Connect to live application</AppButton></template>
    </BaseModal>
  </div>
</template>
<script setup lang="ts">
import { apiErrorMessage } from '../../utils/apiError';
import { ref, onBeforeUnmount, onDeactivated, watch, nextTick } from 'vue';
import { useRoute, onBeforeRouteLeave } from 'vue-router';
import { apiClient } from '../../api/client';
import { useAuthStore } from '../../stores/auth';
import InteractiveTerminal from '../../components/InteractiveTerminal.vue';
import Card from '../../components/Card.vue';
import PageHeader from '../../components/PageHeader.vue';
import ErrorAlert from '../../components/ErrorAlert.vue';
import AppButton from '../../components/AppButton.vue';
import BaseModal from '../../components/BaseModal.vue';
import FormGroup from '../../components/FormGroup.vue';
import StatusBadge from '../../components/StatusBadge.vue';
const route = useRoute(); const auth = useAuthStore();
const site = ref<any>(); const error = ref(''); const ended = ref(''); const password = ref(''); const showPassword = ref(false);
const connecting = ref(false); const connected = ref(false); const status = ref('Disconnected');
const socket = ref<WebSocket | null>(null); const terminal = ref<InstanceType<typeof InteractiveTerminal>>();
let generation = 0; let metadataGeneration = 0; let session: { id: string; siteId: string } | undefined;
async function removeSession(value: typeof session) { if (value) { try { await apiClient.deleteTinkerSession(value.siteId, value.id); } catch { /* Socket close also ends the execution domain. */ } } }
function disconnect() {
  generation++; password.value = ''; showPassword.value = false;
  const previous = session; session = undefined;
  const ws = socket.value; socket.value = null; ws?.close(); void removeSession(previous);
  connected.value = false; connecting.value = false; status.value = 'Disconnected';
}
function openConnect() { error.value = ''; showPassword.value = true; }
function reset() { disconnect(); error.value = ''; ended.value = "Previous session ended."; showPassword.value = true; }
function outputOverflow() { disconnect(); error.value = 'Console output exceeded the browser render limit. The session was disconnected.'; }
async function connect() {
  if (connecting.value || socket.value) return;
  const current = ++generation; const siteId = String(route.params.id);
  connecting.value = true; error.value = ''; ended.value = '';
  const secret = password.value; password.value = '';
  try {
    const grant = await apiClient.createTinkerSession(siteId, secret);
    const pending = { id: grant.session_id, siteId };
    if (current !== generation) { await removeSession(pending); return; }
    session = pending; showPassword.value = false;
    const query = new URLSearchParams({ session: grant.session_id, ticket: grant.ticket });
    const ws = new WebSocket(`wss://${window.location.host}/api/v1/tinker/ws?${query}`); ws.binaryType = 'arraybuffer';
    socket.value = ws; status.value = 'Connecting';
    ws.onopen = async () => { if (current !== generation) return; connected.value = true; connecting.value = false; status.value = 'Connected'; await nextTick(); terminal.value?.resize(); terminal.value?.focus(); };
    ws.onmessage = event => { if (current === generation && event.data instanceof ArrayBuffer) terminal.value?.write(new Uint8Array(event.data)); };
    ws.onerror = () => { if (current === generation) error.value = 'Could not connect. Check HTTPS, Tinker installation and server availability, then reconnect.'; };
    ws.onclose = event => { if (current !== generation) return; disconnect(); ended.value = event.reason || 'The session ended.'; };
  } catch (e: any) { if (current === generation) { error.value = apiErrorMessage(e, 'Connection failed'); connecting.value = false; } }
}
function acknowledgeOutput(bytes: number) { if (socket.value?.readyState === WebSocket.OPEN) socket.value.send(JSON.stringify({ type: 'ack', bytes })); }
function sendInput(data: string) { if (socket.value?.readyState === WebSocket.OPEN) { if (socket.value.bufferedAmount > 65536) { disconnect(); error.value = 'Connection is too slow. Reconnect to continue.'; return; } socket.value.send(JSON.stringify({ type: 'input', data })); } }
function sendResize(rows: number, cols: number) { if (socket.value?.readyState === WebSocket.OPEN) socket.value.send(JSON.stringify({ type: 'resize', rows: Math.max(2, Math.min(200, rows)), cols: Math.max(10, Math.min(400, cols)) })); }
watch(showPassword, value => { if (!value) password.value = ''; });
watch(() => auth.token, () => disconnect());
watch(() => route.params.id, async id => {
  disconnect();
  const current = ++metadataGeneration;
  site.value = undefined; error.value = ''; ended.value = '';
  if (!id) return;
  const siteId = String(id);
  try {
    const value = await apiClient.getSite(siteId);
    if (current === metadataGeneration && String(route.params.id) === siteId) site.value = value;
  } catch (e: any) {
    if (current === metadataGeneration && String(route.params.id) === siteId) error.value = apiErrorMessage(e);
  }
}, { immediate: true });
onBeforeRouteLeave(() => { disconnect(); });
onBeforeUnmount(() => { metadataGeneration++; disconnect(); });
onDeactivated(disconnect);
</script>
