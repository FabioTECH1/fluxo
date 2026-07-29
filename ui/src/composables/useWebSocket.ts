import { ref } from 'vue'
import { useAuthStore } from '../stores/auth'

export function useWebSocket() {
  const logs = ref<string[]>([])
  const isConnected = ref(false)
  let ws: WebSocket | null = null
  let buf: string[] = []
  let flushTimer: number | null = null

  function connect(siteId: string | number, options: { deploymentId?: string | number; commandId?: string | number; replay?: boolean } = {}) {
    disconnect()
    if (!/^[1-9]\d*$/.test(String(siteId))) return
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const token = useAuthStore().token
    const params = new URLSearchParams({
      site_id: String(siteId),
      token: token || '',
    })
    if (options.deploymentId && /^[1-9]\d*$/.test(String(options.deploymentId))) {
      params.set('deployment_id', String(options.deploymentId))
    }
    if (options.commandId && /^[1-9]\d*$/.test(String(options.commandId))) {
      params.set('command_id', String(options.commandId))
    }
    if (options.replay) {
      params.set('replay', '1')
    }
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws?${params.toString()}`)
    ws = socket

    socket.onopen = () => {
      if (ws === socket) isConnected.value = true
    }

    socket.onmessage = (event) => {
      if (ws !== socket) return
      buf.push(String(event.data))
      if (!flushTimer) {
        flushTimer = window.setTimeout(() => {
          logs.value.push(...buf)
          buf = []
          flushTimer = null
        }, 100)
      }
    }

    socket.onclose = () => {
      if (ws === socket) isConnected.value = false
    }
    socket.onerror = () => {
      if (ws === socket) isConnected.value = false
    }
  }

  function disconnect() {
    if (flushTimer) { clearTimeout(flushTimer); flushTimer = null }
    if (buf.length > 0) {
      logs.value.push(...buf)
      buf = []
    }
    ws?.close()
    ws = null
    isConnected.value = false
  }

  function clear() {
    if (flushTimer) { clearTimeout(flushTimer); flushTimer = null }
    logs.value = []
    buf = []
  }

  return { logs, isConnected, connect, disconnect, clear }
}
