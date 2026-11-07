import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { renderWithProviders } from '../../testing';
import { COSTE360_FILES, COSTE360_URL, MadeByCoste360 } from './MadeByCoste360';

describe('MadeByCoste360', () => {
  it('links to coste360.it in a new tab with an accessible name', () => {
    renderWithProviders(<MadeByCoste360 />);
    const link = screen.getByRole('link', { name: /un prodotto di Coste360/i });
    expect(link).toHaveAttribute('href', COSTE360_URL);
    expect(link).toHaveAttribute('target', '_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(link).toHaveTextContent('un prodotto di');
  });

  it('shows the official light and dark logo files, switched by the theme class', () => {
    const { container } = renderWithProviders(<MadeByCoste360 />);
    const imgs = Array.from(container.querySelectorAll('img'));
    expect(imgs.map((i) => i.getAttribute('src'))).toEqual([
      COSTE360_FILES.light,
      COSTE360_FILES.dark,
    ]);
    expect(imgs[0]?.className).toContain('dark:hidden');
    expect(imgs[1]?.className).toContain('dark:block');
    expect(imgs.every((i) => i.getAttribute('alt') === '')).toBe(true);
  });
});
