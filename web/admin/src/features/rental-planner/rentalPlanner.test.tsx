import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import type { RentalPlanner } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, baseRoutes } from '../desk/test/fixtures';
import { todayIn } from '../registry/lib/today';

const PERMS = ['marina.read', 'rental.manage', 'map.view'] as const;

const planner = (from: string): RentalPlanner => ({
  from,
  to: from,
  view: 'hours',
  open_from_slot: 16,
  open_to_slot: 38,
  buffer_slots: 1,
  bands: [
    { code: 'morning', from_slot: 16, to_slot: 26, label: { it: 'Mattina' } },
    { code: 'afternoon', from_slot: 28, to_slot: 38, label: { it: 'Pomeriggio' } },
  ],
  rows: [
    {
      id: 'u1',
      kind: 'unit',
      code: 'GOM58',
      category: 'rib',
      status: 'available',
      seats: 8,
      segments: [
        {
          booking_id: 'b1',
          kind: 'booked',
          status: 'confirmed',
          number: 'NL2026-000001',
          from: { date: from, slot: 30 },
          to: { date: from, slot: 34 },
        },
        {
          booking_id: 'b1',
          kind: 'buffer',
          status: 'confirmed',
          from: { date: from, slot: 34 },
          to: { date: from, slot: 35 },
        },
      ],
    },
    { id: 's1', kind: 'skipper', code: 'Marco', segments: [] },
  ],
});

describe('rental planner page', () => {
  beforeAll(() => {
    Element.prototype.scrollIntoView = vi.fn();
    Element.prototype.hasPointerCapture = vi.fn(() => false);
  });

  it('drags a range, suggests the formula and shows a 409 with the booking highlighted', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/rental/planner`, [
      ...baseRoutes(makeMe({}, [...PERMS])),
      { path: `${API}/rental/planner`, body: planner(todayIn('Europe/Rome')) },
      {
        path: `${API}/rental/formulas`,
        body: {
          items: [
            { id: 'f1', code: 'ORARIA', kind: 'hourly', active: true, categories: [] },
            { id: 'f2', code: 'GIORNATA', kind: 'day', active: true, categories: [] },
          ],
        },
      },
      {
        path: `${API}/rental/skippers`,
        body: { items: [{ id: 's1', name: 'Marco', active: true }], next_cursor: null },
      },
      { path: `${API}/rental/extras`, body: { items: [] } },
      {
        method: 'POST',
        path: `${API}/rental/quote`,
        body: {
          items: [
            {
              formula_id: 'f2',
              formula_code: 'GIORNATA',
              kind: 'day',
              ok: false,
              problem: 'formula_range_mismatch',
              total_cents: 0,
              licence_required: true,
              lines: [],
            },
            {
              formula_id: 'f1',
              formula_code: 'ORARIA',
              kind: 'hourly',
              ok: true,
              total_cents: 9000,
              licence_required: true,
              lines: [],
            },
          ],
        },
      },
      {
        method: 'POST',
        path: `${API}/rental/bookings`,
        status: 409,
        body: {
          type: 'rental_slot_taken',
          title: 'taken',
          status: 409,
          details: { target: 'unit', booking_ids: ['b1'], slots: [30] },
        },
      },
    ]);
    const booking = await screen.findByRole('button', { name: /GOM58: NL2026-000001 Confermato/ });
    expect(booking.querySelector('rect')?.getAttribute('class')).toContain('fill-chart-1');
    // Cells start at 06:00 (slot 12): cells 8..11 are 10:00-12:00.
    const cells = screen.getAllByTestId(/^planner-cell-GOM58-/);
    expect(cells).toHaveLength(30);
    fireEvent.pointerDown(screen.getByTestId('planner-cell-GOM58-8'), { button: 0 });
    fireEvent.pointerEnter(screen.getByTestId('planner-cell-GOM58-11'));
    fireEvent.pointerUp(screen.getByRole('application'));
    const dialog = await screen.findByTestId('rental-booking-dialog');
    expect(dialog).toHaveTextContent('GOM58, 10:00-12:00');
    const hourly = await within(dialog).findByRole('radio', { name: /ORARIA/ });
    await waitFor(() => expect(hourly).toBeChecked());
    expect(within(dialog).getByRole('radio', { name: /GIORNATA/ })).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText('Nome del cliente'), 'Ada');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Opzione' }));
    expect(await within(dialog).findByTestId('rental-booking-conflict')).toBeInTheDocument();
    const post = calls.find(
      (c) => c.method === 'POST' && c.url.pathname.endsWith('/rental/bookings'),
    );
    expect(post?.body).toMatchObject({
      unit_id: 'u1',
      formula_id: 'f1',
      hold: true,
      start: { slot: 20 },
      end: { slot: 24 },
      renter: { name: 'Ada' },
    });
    await waitFor(() =>
      expect(
        document.querySelector('[data-booking-id="b1"] rect')?.getAttribute('class'),
      ).toContain('stroke-destructive'),
    );
  });
});
