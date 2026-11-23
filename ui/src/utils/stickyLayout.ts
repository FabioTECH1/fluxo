// Header is the only sticky ancestor. Persistent notices stay in normal flow
// above the page, so a side group cannot cover them as it reaches its threshold.
export function stickyGeometry(width: number, height: number, header: number, content: number, minWidth = 768, scrollable = false) {
  const top = Math.ceil(Math.max(0, header)) + 16;
  const available = Math.max(0, height - top - 16);
  return { top, available, active: width >= minWidth && available >= 160 && (scrollable || content <= available) };
}
