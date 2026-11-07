import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { PriceMatrix, RenewalPreview } from '@porto/shared/api';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { base, makeDocument, pricingRoutes } from '../test/fixtures';

const matrix = (cents: number): PriceMatrix => ({
  price_list_id: 'pl1',
  contract_type: 'transit',
  item_id: 'i1',
  quoted_on: '2027-01-01',
  rows: [{ key: 'L12', kind: 'band', probe_loa_cm: 1200, probe_beam_cm: 400 }],
  columns: [{ key: 'ALTA', arrival: '2027-06-01', departure: '2027-06-02' }],
  cells: [
    {
      row: 0,
      col: 0,
      total_cents: cents,
      probe: { contract_type: 'transit', arrival: '2027-06-01', departure: '2027-06-02' },
    },
  ],
});

const preview: RenewalPreview = {
  document: makeDocument({ code: 'T-2028' }),
  diffs: [
    {
      contract_type: 'transit',
      before: matrix(9000),
      after: matrix(9450),
      cells: [
        { row: 0, col: 0, old_cents: 9000, new_cents: 9450, delta_cents: 450, delta_bp: 500 },
      ],
    },
  ],
  summary: { rules: 1, amounts_changed: 1 },
};

describe('RenewalPage', () => {
  it('goes through target, adjustments, preview and creates the draft', async () => {
    const created = makeDocument({ id: 'pl2', code: 'T-2028', source_price_list_id: 'pl1' });
    const { calls, router } = renderApp(`/m/${MARINA_ID}/price-lists/pl1/renew`, [
      {
        method: 'POST',
        path: (u) =>
          u.pathname === `${base}/price-lists/pl1/renewal` &&
          calls.filter((c) => c.method === 'POST').length === 1,
        body: preview,
      },
      { method: 'POST', path: `${base}/price-lists/pl1/renewal`, status: 201, body: created },
      { path: `${base}/price-lists/pl1`, body: makeDocument() },
      { path: `${base}/price-lists/pl2`, body: created },
      ...pricingRoutes(),
    ]);
    // Step 1: the code moves to the next year; dates follow the shift.
    expect(await screen.findByLabelText('Codice')).toHaveValue('T-2028');
    expect(screen.getByLabelText('Valido dal')).toHaveTextContent('01/01/2028');
    const shift = screen.getByLabelText('Sposta le date di (anni)');
    fireEvent.change(shift, { target: { value: '7' } });
    await userEvent.click(screen.getByRole('button', { name: 'Continua' }));
    expect(
      await screen.findByText('Da 0 a 5 anni.', { selector: '[data-slot="field-error"]' }),
    ).toBeInTheDocument();
    fireEvent.change(shift, { target: { value: '1' } });
    await userEvent.click(screen.getByRole('button', { name: 'Continua' }));
    // Step 2: ISTAT and a +5 % on the whole list.
    fireEvent.change(await screen.findByLabelText('Indice ISTAT'), { target: { value: '1,8' } });
    await userEvent.click(screen.getByRole('button', { name: /Aggiungi adeguamento/ }));
    fireEvent.change(screen.getByLabelText('Valore'), { target: { value: '5' } });
    await userEvent.click(screen.getByRole('button', { name: 'Anteprima' }));
    // Step 3: preview with the matrix diff.
    expect(
      await screen.findByText('Regole copiate: 1 · importi modificati: 1'),
    ).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Transito' })).toBeInTheDocument();
    const dry = calls.find((c) => c.method === 'POST')!;
    expect(dry.body).toEqual({
      dry_run: true,
      code: 'T-2028',
      name: { it: 'Transiti 2027' },
      shift_years: 1,
      istat_bp: 180,
      adjustments: [{ scope: 'all', percent_bp: 500 }],
      amount_rounding: 'none',
    });
    await userEvent.click(screen.getByRole('button', { name: 'Continua' }));
    await userEvent.click(screen.getByRole('button', { name: 'Crea bozza' }));
    await waitFor(() => expect(router.state.location.pathname).toContain('/price-lists/pl2'));
    const apply = calls.filter((c) => c.method === 'POST')[1]!;
    expect(apply.body).toMatchObject({ dry_run: false, code: 'T-2028' });
    expect(apply.headers.get('Idempotency-Key')).toBeTruthy();
  });
});
