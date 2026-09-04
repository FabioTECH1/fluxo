import { ref, computed, watch } from 'vue';

type Theme = 'light' | 'dark' | 'system';

const isBrowser = typeof window !== 'undefined' && typeof document !== 'undefined';
const stored = isBrowser ? localStorage.getItem('fluxo_theme') as Theme | null : null;
const theme = ref<Theme>(stored || 'system');

const mq = isBrowser ? window.matchMedia('(prefers-color-scheme: dark)') : null;

const prefersDark = () => mq?.matches ?? false;

function applyTheme(dark: boolean) {
  if (!isBrowser) return;
  if (dark) {
    document.documentElement.classList.add('dark');
  } else {
    document.documentElement.classList.remove('dark');
  }
}

function syncTheme(t: Theme) {
  if (t === 'system') {
    applyTheme(prefersDark());
  } else {
    applyTheme(t === 'dark');
  }
}

watch(theme, (t) => {
  if (!isBrowser) return;
  localStorage.setItem('fluxo_theme', t);
  syncTheme(t);
}, { immediate: true });

mq?.addEventListener('change', () => {
  if (theme.value === 'system') {
    applyTheme(prefersDark());
  }
});

const isDark = computed(() => {
  if (theme.value === 'dark') return true;
  if (theme.value === 'light') return false;
  return prefersDark();
});

export function useTheme() {
  return { theme, isDark };
}
