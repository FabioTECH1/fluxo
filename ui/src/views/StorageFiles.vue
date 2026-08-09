<template>
  <div class="space-y-4">
    <Card>
      <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div class="min-w-0 flex-1">
          <label for="file-site" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">Site</label>
          <select id="file-site" v-model="selectedSiteId" class="w-full max-w-md rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" @change="changeSite">
            <option value="" disabled>Select a site</option>
            <option v-for="site in sites" :key="site.id" :value="String(site.id)">{{ site.domain }}</option>
          </select>
        </div>
        <div class="flex w-full flex-col gap-4 sm:w-auto sm:flex-row sm:items-center sm:gap-5">
          <div class="shrink-0 [&>label]:items-center [&>label>button]:mt-0">
            <ToggleSwitch v-model="showHidden" label="Hidden files" :disabled="!selectedSiteId" @update:model-value="toggleHidden" />
          </div>
          <div class="flex items-center gap-2 sm:shrink-0">
            <input ref="uploadInput" type="file" multiple class="hidden" @change="uploadFiles" />
            <AppButton variant="secondary" size="sm" :disabled="!selectedSiteId || uploading" :loading="uploading" @click="uploadInput?.click()">Upload</AppButton>
            <AppButton size="sm" :disabled="!selectedSiteId" @click="openCreateModal">New</AppButton>
          </div>
        </div>
      </div>
    </Card>

    <div v-if="zeroDowntimeWarning" class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300">
      Files inside <span class="font-mono">current</span> belong to the active zero-downtime release. A later deployment may replace changes made there.
    </div>

    <Card>
      <div class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Site files</h2>
          <nav v-if="selectedSiteId" class="mt-1 flex min-w-0 flex-wrap items-center gap-1 text-sm" aria-label="File path">
            <template v-for="(crumb, index) in breadcrumbs" :key="crumb.path">
              <span v-if="index > 0" class="text-gray-400">/</span>
              <button type="button" class="max-w-48 truncate rounded px-1 py-0.5 font-mono text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950/40" :title="crumb.label" @click="navigate(crumb.path)">{{ crumb.label }}</button>
            </template>
          </nav>
          <p v-else class="mt-1 text-sm text-gray-500 dark:text-gray-400">Choose a site to browse its files.</p>
        </div>
        <AppButton v-if="currentPath !== '.'" variant="secondary" size="sm" :disabled="loading" @click="navigate(parentPath)">Up one level</AppButton>
      </div>

      <SkeletonLoader v-if="loading" type="table" />
      <ErrorAlert v-else-if="error" :message="error" />
      <template v-else>
        <DataTable
          :columns="columns"
          :items="entries"
          empty-text="This directory is empty."
          aria-label="Site files"
          scroll-class="max-h-[55vh] overflow-y-auto overscroll-y-contain sm:max-h-[60vh] lg:max-h-[65vh]"
          sticky-header
        >
          <template #name="{ item }">
            <button v-if="item.is_directory && !item.unsafe_symlink" type="button" class="flex max-w-sm items-center gap-2 text-left font-medium text-blue-600 hover:underline dark:text-blue-400" @click="navigate(item.path)">
              <svg class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z" /></svg>
              <span class="truncate" :title="item.name">{{ item.name }}</span>
              <span v-if="item.is_symlink" class="text-xs font-normal text-gray-400">link</span>
            </button>
            <div v-else class="flex max-w-sm items-center gap-2 text-gray-900 dark:text-gray-100">
              <svg class="h-5 w-5 shrink-0 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8m-6-6v6h6" /></svg>
              <span class="truncate font-medium" :title="item.name">{{ item.name }}</span>
              <span v-if="item.is_symlink" class="text-xs font-normal" :class="item.unsafe_symlink ? 'text-red-500' : 'text-gray-400'">{{ item.unsafe_symlink ? 'unsafe link' : 'link' }}</span>
            </div>
          </template>
          <template #size="{ item }"><span class="text-gray-500 dark:text-gray-400">{{ item.is_directory ? '—' : formatBytes(item.size) }}</span></template>
          <template #modified="{ item }"><span class="text-gray-500 dark:text-gray-400">{{ formatDate(item.modified) }}</span></template>
          <template #permissions="{ item }"><span class="font-mono text-xs text-gray-500 dark:text-gray-400">{{ item.permissions }}</span></template>
          <template #actions="{ item }"><TableActionMenu :items="menuItems(item)" :aria-label="`Actions for ${item.name}`" @select="handleAction($event, item)" /></template>
        </DataTable>
        <TablePagination v-model:page="page" :total-items="total" :page-size="pageSize" @update:page="loadDirectory" />
      </template>
    </Card>

    <BaseModal
      :model-value="showEditor"
      :title="editorPath"
      max-width="max-w-5xl"
      :loading="saving"
      :prevent-dismiss="true"
      @update:model-value="handleEditorVisibility"
    >
      <template #title>
        <div class="flex min-w-0 items-center gap-2">
          <h3 class="truncate text-lg font-bold text-gray-900 dark:text-gray-100" :title="editorPath">{{ editorPath }}</h3>
          <span v-if="hasUnsavedChanges" class="shrink-0 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-semibold text-amber-800 dark:bg-amber-900/40 dark:text-amber-300">Unsaved</span>
        </div>
      </template>

      <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">UTF-8 text files up to 1 MB. Standard undo, redo, select, copy, cut, and paste shortcuts work here. Ctrl/Cmd+S saves; copy or cut with no selection acts on the current line.</p>
      <ScriptEditor
        v-model="editorContent"
        language="plain"
        label="File content editor"
        :visible-lines="24"
        :minimum-lines="24"
        :readonly="saving"
        :busy="saving"
        @keydown="handleEditorKeydown"
      />
      <div class="mt-2 flex items-center justify-between gap-3 text-xs">
        <span :class="hasUnsavedChanges ? 'font-semibold text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-gray-400'">
          {{ hasUnsavedChanges ? 'Unsaved changes' : 'No unsaved changes' }}
        </span>
        <span class="hidden text-right text-gray-400 sm:inline dark:text-gray-500">Ctrl/Cmd+S to save</span>
      </div>

      <template #footer>
        <AppButton variant="secondary" :disabled="saving || confirmingEditorClose" @click="requestCloseEditor">Close</AppButton>
        <AppButton :loading="saving" :disabled="!hasUnsavedChanges || confirmingEditorClose" @click="saveFile">Save file</AppButton>
      </template>
    </BaseModal>

    <BaseModal v-model="showCreate" title="Create file or folder" :loading="creating" confirm-text="Create" @submit="createEntry">
      <div class="space-y-4">
        <div>
          <label for="entry-type" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">Type</label>
          <select id="entry-type" v-model="newEntryType" class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"><option value="file">File</option><option value="directory">Folder</option></select>
        </div>
        <div>
          <label for="entry-name" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">Name</label>
          <input id="entry-name" v-model.trim="newEntryName" type="text" autocomplete="off" class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" @keyup.enter="createEntry" />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Created inside <span class="font-mono">{{ displayPath }}</span>.</p>
        </div>
      </div>
    </BaseModal>

    <BaseModal v-model="showMove" title="Rename or move" :loading="moving" confirm-text="Move" @submit="moveEntry">
      <label for="move-destination" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">Destination path</label>
      <input id="move-destination" v-model.trim="moveDestination" type="text" autocomplete="off" class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100" @keyup.enter="moveEntry" />
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">Use a path relative to the site's root. Existing destinations are never overwritten.</p>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onMounted, ref } from 'vue';
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router';
import { apiClient } from '../api/client';
import AppButton from '../components/AppButton.vue';
import BaseModal from '../components/BaseModal.vue';
import Card from '../components/Card.vue';
import DataTable from '../components/DataTable.vue';
import ScriptEditor from '../components/ScriptEditor.vue';
import ErrorAlert from '../components/ErrorAlert.vue';
import SkeletonLoader from '../components/SkeletonLoader.vue';
import TableActionMenu from '../components/TableActionMenu.vue';
import TablePagination from '../components/TablePagination.vue';
import ToggleSwitch from '../components/ToggleSwitch.vue';
import { useConfirm } from '../composables/useConfirm';
import { useToast } from '../composables/useToast';

interface Site { id: number; domain: string; deployment_strategy: string }
interface FileEntry {
  name: string; path: string; size: number; permissions: string; modified: string;
  is_directory: boolean; is_file: boolean; is_symlink: boolean; unsafe_symlink: boolean; editable: boolean;
}

const columns = [{ key: 'name', label: 'Name' }, { key: 'size', label: 'Size' }, { key: 'modified', label: 'Modified' }, { key: 'permissions', label: 'Mode' }];
const pageSize = 250;
const route = useRoute();
const router = useRouter();
const { confirm } = useConfirm();
const { addToast } = useToast();
const sites = ref<Site[]>([]);
const selectedSiteId = ref('');
const currentPath = ref('.');
const parentPath = ref('.');
const entries = ref<FileEntry[]>([]);
const total = ref(0);
const page = ref(1);
const showHidden = ref(false);
const loading = ref(false);
const error = ref('');
const uploading = ref(false);
const uploadInput = ref<HTMLInputElement | null>(null);
const showEditor = ref(false);
const editorPath = ref('');
const editorContent = ref('');
const editorOriginalContent = ref('');
const editorSHA256 = ref('');
const saving = ref(false);
const confirmingEditorClose = ref(false);
const showCreate = ref(false);
const newEntryName = ref('');
const newEntryType = ref<'file' | 'directory'>('file');
const creating = ref(false);
const showMove = ref(false);
const moveSource = ref('');
const moveDestination = ref('');
const moving = ref(false);
let directoryRequest = 0;

const selectedSite = computed(() => sites.value.find(site => String(site.id) === selectedSiteId.value));
const displayPath = computed(() => currentPath.value === '.' ? '/' : `/${currentPath.value}`);
const zeroDowntimeWarning = computed(() => selectedSite.value?.deployment_strategy === 'zero-downtime');
const hasUnsavedChanges = computed(() => editorContent.value !== editorOriginalContent.value);
const breadcrumbs = computed(() => {
  const values = [{ label: selectedSite.value?.domain || 'Site root', path: '.' }];
  if (currentPath.value === '.') return values;
  let accumulated = '';
  for (const part of currentPath.value.split('/')) {
    accumulated = accumulated ? `${accumulated}/${part}` : part;
    values.push({ label: part, path: accumulated });
  }
  return values;
});
const joinPath = (directory: string, name: string) => directory === '.' ? name : `${directory}/${name}`;
const errorMessage = (reason: unknown) => reason instanceof Error ? reason.message : 'File operation failed';

const loadDirectory = async () => {
  if (!selectedSiteId.value) return;
  const request = ++directoryRequest;
  const siteId = selectedSiteId.value;
  const requestedPath = currentPath.value;
  loading.value = true;
  error.value = '';
  try {
    const result = await apiClient.getSiteFiles(siteId, requestedPath, showHidden.value, (page.value - 1) * pageSize, pageSize);
    if (request !== directoryRequest) return;
    currentPath.value = result.path;
    parentPath.value = result.parent;
    entries.value = result.entries || [];
    total.value = result.total || 0;
  } catch (reason) {
    if (request !== directoryRequest) return;
    error.value = errorMessage(reason);
    entries.value = [];
    total.value = 0;
  } finally { if (request === directoryRequest) loading.value = false; }
};
const navigate = async (destination: string) => { currentPath.value = destination; page.value = 1; await loadDirectory(); };
const changeSite = async () => {
  if (!selectedSiteId.value) return;
  await router.replace({ path: '/storage/files', query: { site_id: selectedSiteId.value } });
  const preferredPath = selectedSite.value?.deployment_strategy === 'zero-downtime' ? 'current' : '.';
  currentPath.value = preferredPath;
  page.value = 1;
  await loadDirectory();
  if (error.value && preferredPath === 'current') { currentPath.value = '.'; await loadDirectory(); }
};
const toggleHidden = () => { page.value = 1; loadDirectory(); };

const menuItems = (entry: FileEntry) => {
  const items: Array<{ id: string; label: string; variant?: 'default' | 'primary' | 'danger' }> = [];
  if (entry.is_directory && !entry.unsafe_symlink) items.push({ id: 'open', label: 'Open' });
  if (entry.is_file && entry.editable && !entry.is_symlink && !entry.unsafe_symlink) items.push({ id: 'edit', label: 'View / edit', variant: 'primary' });
  if (entry.is_file && !entry.unsafe_symlink) items.push({ id: 'download', label: 'Download' });
  items.push({ id: 'move', label: 'Rename / move' }, { id: 'delete', label: 'Delete', variant: 'danger' });
  return items;
};
const handleAction = (action: string, entry: FileEntry) => {
  if (action === 'open') navigate(entry.path);
  if (action === 'edit') openEditor(entry);
  if (action === 'download') downloadEntry(entry);
  if (action === 'move') { moveSource.value = entry.path; moveDestination.value = entry.path; showMove.value = true; }
  if (action === 'delete') deleteEntry(entry);
};
const openEditor = async (entry: FileEntry) => {
  try {
    const result = await apiClient.getSiteFileContent(selectedSiteId.value, entry.path);
    editorPath.value = result.path;
    editorContent.value = result.content;
    editorOriginalContent.value = result.content;
    editorSHA256.value = result.sha256;
    showEditor.value = true;
  } catch (reason) { addToast(errorMessage(reason), 'error'); }
};
const confirmEditorDiscard = async () => {
  if (!hasUnsavedChanges.value) return true;
  if (confirmingEditorClose.value) return false;
  confirmingEditorClose.value = true;
  try {
    return await confirm({
      title: 'Discard unsaved changes?',
      message: `Changes to ${editorPath.value} have not been saved.`,
      confirmText: 'Discard changes',
      variant: 'danger',
    });
  } finally {
    confirmingEditorClose.value = false;
  }
};
const requestCloseEditor = async () => {
  if (saving.value || !await confirmEditorDiscard()) return;
  editorOriginalContent.value = editorContent.value;
  showEditor.value = false;
};
const handleEditorVisibility = (visible: boolean) => {
  if (visible) {
    showEditor.value = true;
    return;
  }
  void requestCloseEditor();
};
const currentLineRange = (value: string, cursor: number) => {
  const precedingNewline = cursor === 0 ? -1 : value.lastIndexOf('\n', cursor - 1);
  let start = precedingNewline + 1;
  const followingNewline = value.indexOf('\n', cursor);
  const end = followingNewline === -1 ? value.length : followingNewline + 1;

  // Include the separator before the final line so cutting it removes the line itself.
  if (followingNewline === -1 && start > 0) start -= 1;
  return { start, end };
};
const handleEditorKeydown = (event: KeyboardEvent, editor: HTMLTextAreaElement) => {
  if (event.isComposing) return;
  const shortcut = (event.ctrlKey || event.metaKey) && !event.altKey;
  if (!shortcut) return;

  const key = event.key.toLowerCase();
  if (key === 's') {
    event.preventDefault();
    if (hasUnsavedChanges.value && !saving.value) void saveFile();
    return;
  }

  if ((key === 'c' || key === 'x') && editor.selectionStart === editor.selectionEnd) {
    const cursor = editor.selectionStart;
    const { start, end } = currentLineRange(editor.value, cursor);
    editor.setSelectionRange(start, end);

    // Keep the browser's clipboard operation and undo history native.
    if (key === 'c') {
      window.setTimeout(() => {
        if (showEditor.value && document.activeElement === editor) editor.setSelectionRange(cursor, cursor);
      }, 0);
    }
  }
};
const saveFile = async () => {
  if (saving.value || !hasUnsavedChanges.value) return;
  const contentToSave = editorContent.value;
  saving.value = true;
  try {
    await apiClient.saveSiteFileContent(selectedSiteId.value, editorPath.value, contentToSave, editorSHA256.value);
    editorOriginalContent.value = contentToSave;
    addToast('File saved.', 'success');
    await loadDirectory();
  } catch (reason) { addToast(errorMessage(reason), 'error'); } finally { saving.value = false; }
};
const openCreateModal = () => { newEntryName.value = ''; newEntryType.value = 'file'; showCreate.value = true; };
const createEntry = async () => {
  if (creating.value) return;
  if (!newEntryName.value) { addToast('Enter a name.', 'error'); return; }
  if (newEntryName.value === '.' || newEntryName.value === '..' || /[\\/]/.test(newEntryName.value)) { addToast('Enter a name without slashes.', 'error'); return; }
  creating.value = true;
  try {
    await apiClient.createSiteFileEntry(selectedSiteId.value, joinPath(currentPath.value, newEntryName.value), newEntryType.value);
    showCreate.value = false; addToast(`${newEntryType.value === 'directory' ? 'Folder' : 'File'} created.`, 'success'); await loadDirectory();
  } catch (reason) { addToast(errorMessage(reason), 'error'); } finally { creating.value = false; }
};
const moveEntry = async () => {
  if (moving.value) return;
  if (!moveDestination.value) { addToast('Enter a destination path.', 'error'); return; }
  moving.value = true;
  try {
    await apiClient.moveSiteFileEntry(selectedSiteId.value, moveSource.value, moveDestination.value);
    showMove.value = false; addToast('Entry moved.', 'success'); await loadDirectory();
  } catch (reason) { addToast(errorMessage(reason), 'error'); } finally { moving.value = false; }
};
const deleteEntry = async (entry: FileEntry) => {
  const approved = await confirm({ title: `Delete ${entry.name}?`, message: entry.is_directory ? 'Only empty folders can be deleted. This action cannot be undone.' : 'This action cannot be undone.', confirmText: 'Delete', variant: 'danger' });
  if (!approved) return;
  try { await apiClient.deleteSiteFileEntry(selectedSiteId.value, entry.path); addToast('Entry deleted.', 'success'); await loadDirectory(); }
  catch (reason) { addToast(errorMessage(reason), 'error'); }
};
const uploadFiles = async (event: Event) => {
  const input = event.target as HTMLInputElement;
  const files = Array.from(input.files || []);
  if (!files.length) return;
  uploading.value = true;
  try {
    for (const file of files) {
      if (file.size > 100 * 1024 * 1024) { addToast(`${file.name} exceeds the 100 MB upload limit.`, 'error'); continue; }
      try { await apiClient.uploadSiteFile(selectedSiteId.value, currentPath.value, file); addToast(`${file.name} uploaded.`, 'success'); }
      catch (reason) {
        if (!errorMessage(reason).toLowerCase().includes('destination already exists')) { addToast(`${file.name}: ${errorMessage(reason)}`, 'error'); continue; }
        const overwrite = await confirm({ title: `Replace ${file.name}?`, message: 'A file with this name already exists. Replacing it cannot be undone.', confirmText: 'Replace', variant: 'danger' });
        if (overwrite) {
          try { await apiClient.uploadSiteFile(selectedSiteId.value, currentPath.value, file, true); addToast(`${file.name} replaced.`, 'success'); }
          catch (retryReason) { addToast(`${file.name}: ${errorMessage(retryReason)}`, 'error'); }
        }
      }
    }
    await loadDirectory();
  } finally { uploading.value = false; input.value = ''; }
};
const downloadEntry = async (entry: FileEntry) => {
  try {
    const blob = await apiClient.downloadSiteFile(selectedSiteId.value, entry.path);
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a'); anchor.href = url; anchor.download = entry.name; document.body.appendChild(anchor); anchor.click(); anchor.remove(); URL.revokeObjectURL(url);
  } catch (reason) { addToast(errorMessage(reason), 'error'); }
};
const formatBytes = (bytes: number) => {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, unit);
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
};
const formatDate = (value: string) => new Date(value).toLocaleString();
const initialize = async () => {
  try {
    sites.value = await apiClient.getSites();
    const requested = typeof route.query.site_id === 'string' ? route.query.site_id : '';
    selectedSiteId.value = sites.value.some(site => String(site.id) === requested) ? requested : (sites.value[0] ? String(sites.value[0].id) : '');
    if (selectedSiteId.value) await changeSite();
  } catch (reason) { error.value = errorMessage(reason); }
};
const handleBeforeUnload = (event: BeforeUnloadEvent) => {
  if (!showEditor.value || !hasUnsavedChanges.value) return;
  event.preventDefault();
  event.returnValue = '';
};
onBeforeRouteLeave(async () => {
  if (!showEditor.value || !hasUnsavedChanges.value) return true;
  const discard = await confirmEditorDiscard();
  if (discard) {
    editorOriginalContent.value = editorContent.value;
    showEditor.value = false;
  }
  return discard;
});
onMounted(initialize);
onMounted(() => window.addEventListener('beforeunload', handleBeforeUnload));
onBeforeUnmount(() => window.removeEventListener('beforeunload', handleBeforeUnload));
onActivated(() => { if (selectedSiteId.value) loadDirectory(); });
</script>
