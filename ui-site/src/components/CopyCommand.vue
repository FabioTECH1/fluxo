<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'

const props = defineProps<{
  command: string
}>()

const copied = ref(false)
const copyError = ref(false)
let resetTimer: ReturnType<typeof setTimeout> | undefined
let copyAttempt = 0
let unmounted = false

async function copyCommand() {
  const attempt = ++copyAttempt
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(props.command)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = props.command
      textarea.setAttribute('readonly', '')
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      const succeeded = document.execCommand('copy')
      textarea.remove()
      if (!succeeded) throw new Error('Copy command was rejected')
    }
    if (unmounted || attempt !== copyAttempt) return
    copied.value = true
    copyError.value = false
    clearTimeout(resetTimer)
    resetTimer = setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    if (unmounted || attempt !== copyAttempt) return
    copied.value = false
    copyError.value = true
  }
}

onBeforeUnmount(() => {
  unmounted = true
  copyAttempt++
  clearTimeout(resetTimer)
})
</script>

<template>
  <div class="flex min-w-0 items-start gap-2">
    <code
      class="block min-w-0 flex-1 overflow-x-auto whitespace-nowrap py-1 font-mono text-[11px] font-bold text-blue-400 sm:text-[12px]">
      {{ command }}
    </code>
    <button type="button" @click="copyCommand"
      class="flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center rounded-lg border border-gray-800 bg-gray-900 text-gray-400 transition-colors hover:border-gray-700 hover:bg-gray-800 hover:text-gray-200 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-400"
      :title="copied ? 'Copied!' : copyError ? 'Copy failed' : 'Copy command'"
      :aria-label="copied ? 'Command copied' : copyError ? 'Copy failed; try selecting the command manually' : 'Copy command'">
      <svg v-if="copied" class="h-4 w-4 text-emerald-400" fill="none" viewBox="0 0 24 24"
        stroke="currentColor" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m5 13 4 4L19 7" />
      </svg>
      <svg v-else class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
          d="M8 5H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-1M8 5a2 2 0 0 0 2 2h2a2 2 0 0 0 2-2M8 5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2m0 0h2a2 2 0 0 1 2 2v3m2 4H10m0 0 3-3m-3 3 3 3" />
      </svg>
    </button>
    <span class="sr-only" aria-live="polite">{{ copied ? 'Command copied.' : copyError ? 'Copy failed. Select the command manually.' : '' }}</span>
  </div>
</template>
