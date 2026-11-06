import { act, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { THEME_STORAGE_KEY, ThemeProvider, readStoredTheme, resolveTheme } from './ThemeProvider';
import { useTheme } from './useTheme';

function Probe() {
  const { resolvedTheme, setTheme } = useTheme();
  return (
    <button type="button" onClick={() => setTheme('dark')}>
      {resolvedTheme}
    </button>
  );
}

describe('ThemeProvider', () => {
  afterEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove('dark');
  });

  it('resolves system preference', () => {
    expect(resolveTheme('system', true)).toBe('dark');
    expect(resolveTheme('system', false)).toBe('light');
    expect(resolveTheme('light', true)).toBe('light');
  });

  it('applies the dark class and stores the preference', () => {
    const { getByRole } = render(
      <ThemeProvider>
        <Probe />
      </ThemeProvider>,
    );
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    act(() => getByRole('button').click());
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(readStoredTheme()).toBe('dark');
  });

  it('ignores invalid stored values', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'purple');
    expect(readStoredTheme()).toBe('system');
  });
});
