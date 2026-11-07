import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { renderWithProviders } from '@porto/shared/testing';

import { matrix, matrixAfter } from '../test/fixtures';
import { PriceMatrixTable } from './PriceMatrixTable';

describe('PriceMatrixTable', () => {
  it('shows the server totals, the no-price reason and the band headers', () => {
    renderWithProviders(<PriceMatrixTable mode="values" matrix={matrix} />);
    const table = screen.getByTestId('price-matrix');
    expect(within(table).getByRole('columnheader', { name: /BASSA/ })).toBeInTheDocument();
    expect(
      within(table).getByRole('rowheader', { name: /L12.*fino a 12,00 m/ }),
    ).toBeInTheDocument();
    expect(screen.getByTestId('matrix-cell-1-1')).toHaveTextContent('90,00 €');
    expect(screen.getByTestId('matrix-cell-1-0')).toHaveTextContent('48,00 €');
    expect(screen.getByTestId('matrix-cell-0-0')).toHaveTextContent(
      'Nessuna regola di prezzo applicabile',
    );
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('passes the probe of a clicked cell', async () => {
    const onCellClick = vi.fn();
    renderWithProviders(
      <PriceMatrixTable mode="values" matrix={matrix} onCellClick={onCellClick} />,
    );
    await userEvent.click(
      screen.getByRole('button', { name: 'L12 × ALTA: 90,00 €, apri nel simulatore' }),
    );
    expect(onCellClick).toHaveBeenCalledWith(matrix.cells[3]!.probe);
  });

  it('shows old, new and delta in diff mode', () => {
    renderWithProviders(
      <PriceMatrixTable
        mode="diff"
        before={matrix}
        after={matrixAfter}
        cells={[
          { row: 1, col: 1, old_cents: 9000, new_cents: 9450, delta_cents: 450, delta_bp: 500 },
          { row: 0, col: 0 },
        ]}
      />,
    );
    const cell = screen.getByTestId('matrix-diff-1-1');
    expect(cell).toHaveTextContent('94,50 €');
    expect(cell).toHaveTextContent('90,00 €');
    expect(cell).toHaveTextContent('+4,50 € (+5%)');
    expect(screen.getByTestId('matrix-diff-0-0')).toHaveTextContent('Senza prezzo');
    // Cells without a diff row fall back to the matrices.
    expect(screen.getByTestId('matrix-diff-1-0')).toHaveTextContent('50,40 €');
  });

  it('says when the matrix is empty', () => {
    renderWithProviders(
      <PriceMatrixTable mode="values" matrix={{ ...matrix, rows: [], cells: [] }} />,
    );
    expect(screen.getByTestId('matrix-empty')).toBeInTheDocument();
  });
});
