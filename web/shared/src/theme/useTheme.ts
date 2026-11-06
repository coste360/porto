import { useContext } from 'react';

import { ThemeContext, type ThemeContextValue } from './ThemeProvider';

const fallback: ThemeContextValue = {
  theme: 'system',
  resolvedTheme: 'light',
  setTheme: () => undefined,
};

/** Current theme; outside a ThemeProvider it reports the light theme and ignores changes. */
export function useTheme(): ThemeContextValue {
  return useContext(ThemeContext) ?? fallback;
}
