import { screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { MapValidationReport } from '@porto/shared/api';
import { renderWithProviders } from '@porto/shared/testing';

import { ValidationPanel } from './ValidationPanel';

const small: MapValidationReport = {
  errors: [],
  warnings: [
    {
      code: 'berth_below_min_size',
      severity: 'warning',
      element_type: 'berth',
      refs: ['b1'],
      field: 'rect',
      params: { width_cm: 120, length_cm: 600, min_width_cm: 150, min_length_cm: 300 },
    },
  ],
};

describe('ValidationPanel', () => {
  it('shows the minimum berth size warning with the thresholds, without errors', () => {
    renderWithProviders(
      <ValidationPanel report={small} status="ok" codeOf={() => 'A-1'} onPick={vi.fn()} />,
    );
    const panel = screen.getByRole('region', { name: 'Verifica della mappa' });
    expect(within(panel).getByTestId('validation-summary')).toHaveTextContent('Errori: 0');
    expect(
      within(panel).getByText('Posto più piccolo della misura minima (1,50 m × 3,00 m)'),
    ).toBeInTheDocument();
    expect(within(panel).getByRole('button', { name: 'A-1' })).toBeInTheDocument();
  });
});
