import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

const apiTarget = process.env.PORTO_DEV_API ?? 'http://localhost:8080';

export default defineConfig({
  // The server rewrites /b/{slug} to this index.html.
  base: '/b/',
  plugins: [react(), tailwindcss()],
  server: {
    port: 5174,
    strictPort: true,
    proxy: { '/api': { target: apiTarget, changeOrigin: false } },
  },
  build: { outDir: 'dist', emptyOutDir: true, sourcemap: true },
});
