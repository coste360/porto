import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    name: 'booking',
    environment: 'jsdom',
    globals: true,
    setupFiles: ['../shared/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}', 'scripts/**/*.test.mjs'],
  },
});
