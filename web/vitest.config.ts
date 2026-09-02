import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [react()],
  test: {
    coverage: {
      exclude: ['web/src/**/*.test.{ts,tsx}', 'web/src/main.tsx', 'web/src/test/**'],
      include: ['web/src/**/*.{ts,tsx}'],
      provider: 'v8',
      reporter: ['text', 'json-summary', 'html'],
      thresholds: {
        branches: 80,
        functions: 80,
        lines: 80,
        statements: 80,
      },
    },
    environment: 'jsdom',
    include: ['web/src/**/*.test.{ts,tsx}'],
    setupFiles: ['./web/src/test/setup.ts'],
  },
});
