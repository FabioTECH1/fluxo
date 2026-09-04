import { createApp, createSSRApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { landingRouter, demoRouter } from './router'
import './style.css'
import { mockApi } from './api/mock'

function showDemoModal() {
  if (document.getElementById('demo-warning-modal')) return;

  const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const modal = document.createElement('div');
  modal.id = 'demo-warning-modal';
  modal.className = 'fixed inset-0 z-[9999] flex items-center justify-center p-4 bg-gray-900/60 backdrop-blur-sm transition-opacity duration-300 opacity-0';
  
  modal.innerHTML = `
    <div role="dialog" aria-modal="true" aria-labelledby="demo-warning-title" aria-describedby="demo-warning-description" class="bg-white dark:bg-gray-900 rounded-lg border border-gray-100 dark:border-gray-800 shadow-2xl max-w-sm w-full overflow-hidden transform scale-95 transition-transform duration-300 p-6 relative">
      <button id="demo-close-x" type="button" aria-label="Close demo notice" class="absolute top-4 right-4 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 transition-colors cursor-pointer">
        <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
      <div class="flex items-start gap-4">
        <div class="w-12 h-12 rounded-full bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center text-blue-600 dark:text-blue-400 shrink-0">
          <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <div class="space-y-1">
          <h3 id="demo-warning-title" class="text-base font-bold text-gray-900 dark:text-gray-100">Demo Mode</h3>
          <p id="demo-warning-description" class="text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
            This is a read-only live demo of Fluxo. Most changes are disabled; deployment actions are simulated for presentation purposes.
          </p>
        </div>
      </div>
      <div class="flex justify-end gap-3 pt-4">
        <button id="demo-close-btn" type="button" class="px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white font-semibold text-xs transition-colors cursor-pointer shadow-md shadow-blue-600/10">
          Got it
        </button>
      </div>
    </div>
  `;

  document.body.appendChild(modal);

  let closing = false;
  const focusable = () => Array.from(modal.querySelectorAll<HTMLElement>('button, [href], [tabindex]:not([tabindex="-1"])'));

  const handleKeydown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeModal();
      return;
    }
    if (event.key !== 'Tab') return;
    const elements = focusable();
    if (elements.length === 0) return;
    const first = elements[0];
    const last = elements[elements.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };

  setTimeout(() => {
    modal.classList.remove('opacity-0');
    const child = modal.firstElementChild;
    if (child) child.classList.remove('scale-95');
    modal.querySelector<HTMLElement>('#demo-close-btn')?.focus();
  }, 10);

  function closeModal() {
    if (closing) return;
    closing = true;
    document.removeEventListener('keydown', handleKeydown);
    modal.classList.add('opacity-0');
    const child = modal.firstElementChild;
    if (child) child.classList.add('scale-95');
    setTimeout(() => {
      modal.remove();
      previouslyFocused?.focus();
    }, 300);
  }

  document.addEventListener('keydown', handleKeydown);

  modal.addEventListener('click', (e) => {
    if (e.target === modal) closeModal();
  });

  modal.querySelector('#demo-close-btn')?.addEventListener('click', closeModal);
  modal.querySelector('#demo-close-x')?.addEventListener('click', closeModal);
}

// Register fetch interceptor for API calls in the demo
const originalFetch = window.fetch;
window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
  
  if (url.startsWith('/api/v1/')) {
    const method = init?.method || 'GET';
    let bodyObj = undefined;
    if (init?.body) {
      try {
        bodyObj = JSON.parse(init.body as string);
      } catch (e) {}
    }
    
    const isPanelDomainAction = (method === 'POST' || method === 'DELETE')
      && /^\/api\/v1\/settings\/panel-domain(?:\/(?:letsencrypt|custom|clone))?$/.test(url);
    const isSimulatedAction = isPanelDomainAction || method === 'POST' && (
      /^\/api\/v1\/sites\/\d+\/deploy$/.test(url) ||
      /^\/api\/v1\/sites\/\d+\/deployments\/\d+\/dismiss$/.test(url) ||
      url.startsWith('/api/v1/system/logs/clear?')
    );
    if (method !== 'GET' && !url.includes('/auth/login') && !url.includes('/auth/bootstrap') && !isSimulatedAction) {
      showDemoModal();
      return new Response('Action restricted in Demo Mode.', {
        status: 403,
        headers: { 'Content-Type': 'text/plain' }
      });
    }

    let data;
    try {
      if (method === 'GET') {
        data = await mockApi.get(url);
      } else if (method === 'POST') {
        data = await mockApi.post(url, bodyObj);
      } else if (method === 'PUT') {
        data = await mockApi.put(url, bodyObj);
      } else if (method === 'DELETE') {
        data = await mockApi.delete(url);
      }
    } catch (err) {
      console.error('Mock API Error:', err);
      return new Response(JSON.stringify({ error: 'Mock API error' }), { status: 500 });
    }

    if (method === 'GET' && url.startsWith('/api/v1/system/logs/download?')) {
      return new Response(String(data ?? ''), {
        status: 200,
        headers: { 'Content-Type': 'text/plain; charset=utf-8' }
      });
    }

    return new Response(JSON.stringify(data), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    });
  }
  
  return originalFetch(input, init);
};

// Dispatch: mount the demo dashboard app when URL is under /demo,
// otherwise mount the landing page. Using two separate Vue apps with
// separate router instances means route.path in the demo app is always
// the unprefixed form (/sites, /overview, etc.), so all active-state
// checks in the core ui components work correctly without modification.
const isDemo = window.location.pathname.startsWith('/demo')

if (isDemo) {
  // Ensure mock JWT exists for auth checks in the dashboard
  if (!localStorage.getItem('fluxo_jwt')) {
    localStorage.setItem('fluxo_jwt', 'demo_token_123')
  }
  createApp(App).use(createPinia()).use(demoRouter).mount('#app')
} else {
  const root = document.querySelector('#app')
  const app = root?.hasAttribute('data-server-rendered') ? createSSRApp(App) : createApp(App)
  app.use(landingRouter)
  landingRouter.isReady().then(() => app.mount('#app'))
}
