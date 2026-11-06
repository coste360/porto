import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    projects: [
      'shared',
      'admin',
      'booking',
      {
        test: { name: 'scripts', environment: 'node', include: ['scripts/**/*.test.mjs'] },
      },
    ],
  },
});
