import js from '@eslint/js';
import i18next from 'eslint-plugin-i18next';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import globals from 'globals';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  {
    ignores: ['**/dist/**', '**/node_modules/**', 'shared/api/generated/**', 'coverage/**'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: { ...globals.browser },
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_' },
      ],
      '@typescript-eslint/consistent-type-imports': 'error',
    },
  },
  {
    // User-facing strings go through i18n keys (Italian source, English required).
    files: ['admin/src/**/*.tsx', 'booking/src/**/*.tsx', 'shared/src/ui/patterns/**/*.tsx'],
    ignores: ['**/*.test.tsx'],
    plugins: { i18next },
    rules: {
      'i18next/no-literal-string': [
        'error',
        {
          mode: 'jsx-text-only',
          'jsx-attributes': { include: ['alt', 'aria-label', 'title', 'placeholder', 'label'] },
        },
      ],
    },
  },
  {
    // shadcn CLI output is kept as generated (see shared/src/ui/README.md): the React
    // Compiler rules flag upstream patterns (Math.random skeleton width, matchMedia sync).
    files: ['shared/src/ui/*.tsx', 'shared/src/hooks/*.ts'],
    rules: { 'react-hooks/purity': 'off', 'react-hooks/set-state-in-effect': 'off' },
  },
  {
    // Library package and shadcn CLI output: fast refresh boundaries are owned by the apps.
    files: ['shared/**/*.{ts,tsx}', '**/*.test.{ts,tsx}', '**/router.tsx'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
  {
    files: ['**/*.{js,mjs}', '**/vite*.config.ts', '**/vitest.config.ts', '**/scripts/**'],
    languageOptions: { globals: { ...globals.node } },
  },
);
