// Module-level singleton state — all callers share the same instance
let originalHref = ''
let bgImage: HTMLImageElement | null = null

function init() {
  if (originalHref) return
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) return
  originalHref = link.getAttribute('href') || ''
  if (!originalHref) return

  const img = new Image()
  img.onload = () => { bgImage = img }
  img.src = originalHref
}

function drawDot(color: string) {
  init()
  const canvas = document.createElement('canvas')
  canvas.width = 32
  canvas.height = 32
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  if (bgImage) {
    ctx.drawImage(bgImage, 0, 0, 32, 32)
  }

  ctx.beginPath()
  ctx.arc(26, 26, 7, 0, Math.PI * 2)
  ctx.fillStyle = color
  ctx.fill()
  ctx.strokeStyle = 'rgba(255,255,255,0.6)'
  ctx.lineWidth = 1.5
  ctx.stroke()

  const dataUrl = canvas.toDataURL('image/png')
  const links = document.querySelectorAll<HTMLLinkElement>('link[rel="icon"]')
  links.forEach(l => l.setAttribute('href', dataUrl))
}

function setDeploying() { drawDot('#f59e0b') }
function setSuccess() { drawDot('#10b981') }
function setFailed() { drawDot('#ef4444') }
function reset() {
  init()
  if (!originalHref) return
  const links = document.querySelectorAll<HTMLLinkElement>('link[rel="icon"]')
  links.forEach(l => l.setAttribute('href', originalHref))
}

export function useFavicon() {
  return { setDeploying, setSuccess, setFailed, reset, init }
}
