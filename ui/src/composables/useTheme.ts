import { ref, computed, watch } from 'vue';

type Theme = 'light' | 'dark' | 'system';

const stored = localStorage.getItem('fluxo_theme') as Theme | null;
const theme = ref<Theme>(stored || 'system');

const mq = window.matchMedia('(prefers-color-scheme: dark)');

const prefersDark = () => mq.matches;

function applyTheme(dark: boolean) {
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
  localStorage.setItem('fluxo_theme', t);
  syncTheme(t);
}, { immediate: true });

mq.addEventListener('change', () => {
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
