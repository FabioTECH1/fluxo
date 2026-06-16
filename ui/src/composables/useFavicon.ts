export function useFavicon() {
  let originalHref = ''

  function init() {
    if (originalHref) return
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!link) return
    originalHref = link.getAttribute('href') || ''
  }

  function drawFavicon(color: string) {
    init()
    const canvas = document.createElement('canvas')
    canvas.width = 32
    canvas.height = 32
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    ctx.beginPath()
    ctx.arc(16, 16, 14, 0, Math.PI * 2)
    ctx.fillStyle = color
    ctx.fill()
    ctx.strokeStyle = 'rgba(0,0,0,0.12)'
    ctx.lineWidth = 1.5
    ctx.stroke()

    const dataUrl = canvas.toDataURL('image/png')
    const links = document.querySelectorAll<HTMLLinkElement>('link[rel="icon"]')
    links.forEach(l => l.setAttribute('href', dataUrl))
  }

  function setDeploying() { drawFavicon('#f59e0b') }
  function setSuccess() { drawFavicon('#10b981') }
  function setFailed() { drawFavicon('#ef4444') }
  function reset() {
    init()
    if (!originalHref) return
    const links = document.querySelectorAll<HTMLLinkElement>('link[rel="icon"]')
    links.forEach(l => l.setAttribute('href', originalHref))
  }

  return { setDeploying, setSuccess, setFailed, reset, init }
}
