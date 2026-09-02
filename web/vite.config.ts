import { resolve } from 'node:path';

import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

const repositoryRoot = resolve(import.meta.dirname, '..');

export default defineConfig({
  root: import.meta.dirname,
  plugins: [react(), tailwindcss()],
  build: {
    emptyOutDir: true,
    outDir: resolve(repositoryRoot, 'internal/dashboard/dist'),
  },
  server: {
    host: process.env.VITE_HOST ?? '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/docs': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
      '/openapi': 'http://127.0.0.1:8080',
      '/readyz': 'http://127.0.0.1:8080',
      '/schemas': 'http://127.0.0.1:8080',
      '/v1': 'http://127.0.0.1:8080',
    },
  },
});
