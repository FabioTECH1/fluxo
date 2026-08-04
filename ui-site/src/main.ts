import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { landingRouter, demoRouter } from './router'
import './style.css'
import { mockApi } from './api/mock'

function showDemoModal() {
  if (document.getElementById('demo-warning-modal')) return;

  const modal = document.createElement('div');
  modal.id = 'demo-warning-modal';
  modal.className = 'fixed inset-0 z-[9999] flex items-center justify-center p-4 bg-gray-900/60 backdrop-blur-sm transition-opacity duration-300 opacity-0';
  
  modal.innerHTML = `
    <div class="bg-white dark:bg-gray-900 rounded-2xl border border-gray-100 dark:border-gray-800 shadow-2xl max-w-sm w-full overflow-hidden transform scale-95 transition-transform duration-300 p-6 relative">
      <button id="demo-close-x" class="absolute top-4 right-4 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 transition-colors cursor-pointer">
        <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
      <div class="flex items-start gap-4">
        <div class="w-12 h-12 rounded-full bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center text-blue-600 dark:text-blue-400 shrink-0">
          <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <div class="space-y-1">
          <h3 class="text-base font-bold text-gray-900 dark:text-gray-100">Demo Mode</h3>
          <p class="text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
            This is a read-only live demo of Fluxo. Database operations, deployments, and setting modifications are simulated for presentation purposes.
          </p>
        </div>
      </div>
      <div class="flex justify-end gap-3 pt-4">
        <button id="demo-close-btn" class="px-4 py-2 rounded-xl bg-blue-600 hover:bg-blue-700 text-white font-semibold text-xs transition-colors cursor-pointer shadow-md shadow-blue-600/10">
          Got it
        </button>
      </div>
    </div>
  `;

  document.body.appendChild(modal);

  setTimeout(() => {
    modal.classList.remove('opacity-0');
    const child = modal.firstElementChild;
    if (child) child.classList.remove('scale-95');
  }, 10);

  const closeModal = () => {
    modal.classList.add('opacity-0');
    const child = modal.firstElementChild;
    if (child) child.classList.add('scale-95');
    setTimeout(() => modal.remove(), 300);
  };

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
    
    const isSimulatedDeploymentAction = method === 'POST' && (
      /^\/api\/v1\/sites\/\d+\/deploy$/.test(url) ||
      /^\/api\/v1\/sites\/\d+\/deployments\/\d+\/dismiss$/.test(url)
    );
    if (method !== 'GET' && !url.includes('/auth/login') && !url.includes('/auth/bootstrap') && !isSimulatedDeploymentAction) {
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
  createApp(App).use(landingRouter).mount('#app')
}
