export function useFavicon() {
  let originalHref = ''
  let cachedImage: HTMLImageElement | null = null

  function init() {
    if (cachedImage) return
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!link) return
    originalHref = link.getAttribute('href') || ''
    if (!originalHref) return

    const img = new Image()
    img.onload = () => { cachedImage = img }
    img.src = originalHref
  }

  function drawDotFavicon(color: string) {
    init()
    const canvas = document.createElement('canvas')
    canvas.width = 32
    canvas.height = 32
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    if (cachedImage) {
      ctx.drawImage(cachedImage, 0, 0, 32, 32)
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

  function setDeploying() { drawDotFavicon('#f59e0b') }
  function setSuccess() { drawDotFavicon('#10b981') }
  function setFailed() { drawDotFavicon('#ef4444') }
  function reset() {
    if (!originalHref) return
    const links = document.querySelectorAll<HTMLLinkElement>('link[rel="icon"]')
    links.forEach(l => l.setAttribute('href', originalHref))
  }

  return { setDeploying, setSuccess, setFailed, reset, init }
}
