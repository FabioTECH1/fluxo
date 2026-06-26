import { ref } from 'vue'

export function useWebSocket() {
  const logs = ref<string[]>([])
  const isConnected = ref(false)
  let ws: WebSocket | null = null
  let buf: string[] = []
  let flushTimer: number | null = null

  function connect(siteId: string | number) {
    disconnect()
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const token = localStorage.getItem('fluxo_jwt') || ''
    ws = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws?site_id=${siteId}&token=${token}`)
    isConnected.value = true

    ws.onmessage = (event) => {
      buf.push(event.data)
      if (!flushTimer) {
        flushTimer = window.setTimeout(() => {
          logs.value.push(...buf)
          buf = []
          flushTimer = null
        }, 100)
      }
    }

    ws.onclose = () => { isConnected.value = false }
    ws.onerror = () => { isConnected.value = false }
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
    logs.value = []
    buf = []
  }

  return { logs, isConnected, connect, disconnect, clear }
}
