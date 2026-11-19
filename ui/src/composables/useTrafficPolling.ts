import { onActivated, onDeactivated, onMounted, onUnmounted } from 'vue';

// One timer per visible analytics screen. Disabled collection needs only manual refresh.
export function useTrafficPolling(load: () => Promise<void>, delay: () => number | null) {
  let active = false;
  let generation = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const clear = () => { if (timer) clearTimeout(timer); timer = undefined; };
  const refresh = async () => {
    clear();
    if (!active || document.hidden) return;
    const current = ++generation;
    await load();
    if (current !== generation || !active || document.hidden) return;
    const milliseconds = delay();
    if (milliseconds !== null) timer = setTimeout(() => { void refresh(); }, milliseconds);
  };
  const start = () => {
    if (active) return;
    active = true;
    if (!document.hidden) void refresh();
  };
  const stop = () => { active = false; generation++; clear(); };
  const visibility = () => {
    generation++; clear();
    if (active && !document.hidden) void refresh();
  };
  onMounted(() => { document.addEventListener('visibilitychange', visibility); start(); });
  onActivated(start);
  onDeactivated(stop);
  onUnmounted(() => { stop(); document.removeEventListener('visibilitychange', visibility); });
  return { refresh };
}
