import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  // Generated OpenAPI types are machine-written; do not lint.
  { ignores: ['dist', 'src/api/generated.ts'] },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // Scoped down to warn: context files legitimately export both the Provider
      // component and its `useXxx` hook; splitting them would touch every consumer.
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      // Scoped down: every page fetches data on mount via `useEffect(() => { load() })`
      // and sets state from the response — the standard pre-Suspense/router-loader
      // pattern used throughout this codebase. Migrating all of it (Suspense
      // boundaries or router loaders) would be a mass rewrite; revisit then.
      'react-hooks/set-state-in-effect': 'off',
      // no-undef cannot reason about TS types and reports false positives for
      // ambient globals (import.meta.env); TS itself flags undefined names.
      'no-undef': 'off',
    },
  },
)
