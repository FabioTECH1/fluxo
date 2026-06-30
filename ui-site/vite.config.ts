import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: {
      '@fluxo': path.resolve(__dirname, '../ui/src'),
    },
  },
  server: {
    // Serve index.html for all routes so the client-side router handles them
    historyApiFallback: true,
  },
})

