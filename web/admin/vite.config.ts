import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig, type Plugin } from 'vite';

const apiTarget = process.env.PORTO_DEV_API ?? 'http://localhost:8080';

/** Adds the service worker registration (src/pwa/register.ts) to index.html as its own module (m6b-plan §3.2). */
function pwaRegister(): Plugin {
  return {
    name: 'porto-pwa-register',
    // 'pre': the injected script is bundled like the one written in index.html.
    transformIndexHtml: {
      order: 'pre',
      handler: () => [
        { tag: 'script', attrs: { type: 'module', src: '/src/pwa/register.ts' }, injectTo: 'body' },
      ],
    },
  };
}

export default defineConfig({
  base: '/admin/',
  plugins: [react(), tailwindcss(), pwaRegister()],
  server: {
    port: 5173,
    strictPort: true,
    // The API (SSE stream included) runs on the Go server in development.
    proxy: {
      '/api': { target: apiTarget, changeOrigin: false },
      '/healthz': apiTarget,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true,
    chunkSizeWarningLimit: 1500,
    // dist/.vite/manifest.json feeds scripts/sw-precache.mjs (never served, internal/http/static).
    manifest: true,
    rollupOptions: {
      input: { index: 'index.html', sw: 'src/sw/sw.ts' },
      output: {
        // The service worker keeps a fixed name (dist/sw.js, scope /admin/); everything else is hashed.
        entryFileNames: (chunk) => (chunk.name === 'sw' ? 'sw.js' : 'assets/[name]-[hash].js'),
      },
    },
  },
});
