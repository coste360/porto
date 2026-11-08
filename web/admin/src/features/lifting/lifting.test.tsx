import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import type { EquipmentBookingDay } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, baseRoutes } from '../desk/test/fixtures';

const PERMS = ['marina.read', 'yard.read', 'yard.manage', 'map.view'] as const;

const day: EquipmentBookingDay = {
  date: '2026-11-03',
  window_from_slot: 28,
  window_to_slot: 76,
  equipment: [
    {
      id: 'tl',
      code: 'TL-70',
      name: 'Travel lift',
      kind: 'travel_lift',
      out_of_service: false,
      default_minutes: 30,
    },
    {
      id: 'gr',
      code: 'GR-1',
      name: 'Gru',
      kind: 'crane',
      out_of_service: true,
      default_minutes: 30,
    },
  ],
  bookings: [
    {
      id: 'b1',
      equipment_id: 'tl',
      date: '2026-11-03',
      from_slot: 32,
      to_slot: 36,
      purpose: 'haul_out',
      vessel_name: 'Libeccio',
      status: 'booked',
      source: 'office',
      note: '',
      version: 2,
      created_at: '2026-11-01T00:00:00Z',
      updated_at: '2026-11-01T00:00:00Z',
    },
  ],
  requests: [],
};

const routes = (
  extra: { method?: string; path: string; status?: number; body?: unknown }[] = [],
) => [
  ...baseRoutes(makeMe({}, [...PERMS])),
  ...extra,
  { path: `${API}/equipment-bookings`, body: day },
  { path: `${API}/equipment/tl/operators`, body: { equipment_id: 'tl', employee_ids: [] } },
  { path: `${API}/yard/employees`, body: { items: [], next_cursor: null } },
  { path: `${API}/vessels`, body: { items: [], next_cursor: null } },
];

describe('lifting page', () => {
  beforeAll(() => {
    Element.prototype.scrollIntoView = vi.fn();
    Element.prototype.hasPointerCapture = vi.fn(() => false);
  });

  it('lays out the day and shows the taken slots of a 409', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/yard/lifting`,
      routes([
        {
          method: 'POST',
          path: `${API}/equipment-bookings`,
          status: 409,
          body: {
            type: 'equipment_slot_taken',
            title: 'taken',
            status: 409,
            details: {
              equipment_id: 'tl',
              date: '2026-11-03',
              slots: [34, 35],
              booking_ids: ['b1'],
            },
          },
        },
      ]),
    );
    const block = await screen.findByTestId('lifting-booking');
    expect(block).toHaveTextContent('08:00 Libeccio');
    expect(screen.getByText('Fuori servizio')).toBeInTheDocument();
    expect(screen.queryByTestId('lifting-free-GR-1-10:00')).toBeNull();
    await userEvent.click(screen.getByTestId('lifting-free-TL-70-10:00'));
    const dialog = await screen.findByTestId('booking-dialog');
    // A haul-out needs a vessel: with purpose "other" none is needed.
    expect(within(dialog).getByRole('button', { name: 'Prenota' })).toBeDisabled();
    fireEvent.keyDown(within(dialog).getByRole('combobox', { name: 'Operazione' }), {
      key: 'Enter',
    });
    await userEvent.click(await screen.findByRole('option', { name: 'Altro' }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Prenota' }));
    expect(await within(dialog).findByTestId('booking-conflict')).toHaveTextContent(
      'Il mezzo è già prenotato in 08:30-09:00.',
    );
    await waitFor(() => {
      const post = calls.find(
        (c) => c.method === 'POST' && c.url.pathname.endsWith('/equipment-bookings'),
      );
      expect(post?.body).toMatchObject({
        equipment_id: 'tl',
        from_slot: 40,
        to_slot: 42,
        purpose: 'other',
      });
    });
  });
});
