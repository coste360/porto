import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { photoError } from '../desk/lib/photo';
import {
  API,
  ALLOC_ID,
  MAP_ID,
  VESSEL_ID,
  baseRoutes,
  meDesk,
  vesselBrief,
} from '../desk/test/fixtures';

vi.mock('../operations/public', () => ({ opsMapSearch: () => ({}), PlacementPicker: () => null }));
vi.mock('../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

const movements = {
  date: '2026-07-10',
  arrivals_expected: [
    {
      vessel: vesselBrief,
      contract: { id: 'c1', kind: 'transit', status: 'confirmed', code: 'TRN-2026-0001' },
      resource: { type: 'berth', id: 'b1', code: 'A01' },
      expected_time: '14:00',
      allocation_id: ALLOC_ID,
    },
  ],
  departures_expected: [],
  vessels_out: [],
  arrived_without_booking: [],
  arrived: [],
  departed: [],
};

/** Phone viewport for the dock panel (useIsMobile reads innerWidth; matchMedia is mocked in setup). */
function phone(width = 360) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width });
}

let innerWidth = 1024;
beforeEach(() => {
  innerWidth = window.innerWidth;
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-07-10T08:00:00Z'));
});
afterEach(() => {
  phone(innerWidth);
  vi.useRealTimers();
});

describe('dock panel', () => {
  it('the old dock module re-exports the phone dock screen', async () => {
    const old = await import('./DockPage');
    const now = await import('../phone/dock/DockPage');
    expect(old.DockPage).toBe(now.DockPage);
  });

  it('records an unexpected arrival of a new vessel with estimated dimensions', async () => {
    phone();
    const { calls } = renderApp(`/m/${MARINA_ID}/dock`, [
      ...baseRoutes(),
      { path: `${API}/movements`, body: movements },
      {
        method: 'POST',
        path: `${API}/presences/unexpected-arrival`,
        status: 201,
        body: { presence: { id: 'p1' } },
      },
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Altro' }));
    await userEvent.click(await screen.findByTestId('dock-more-unexpected'));
    await userEvent.type(await screen.findByLabelText("Nome dell'imbarcazione"), 'Grecale');
    await userEvent.type(screen.getByLabelText('Lunghezza fuori tutto'), '9');
    await userEvent.type(screen.getByLabelText('Larghezza'), '3,2');
    const { pickOption } = await import('../pricing/test/dom');
    await pickOption('Posto o risorsa', /A02/);
    await userEvent.click(screen.getByRole('button', { name: "Registra l'arrivo" }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.pathname.endsWith('/unexpected-arrival'))).toBe(true),
    );
    expect(calls.find((c) => c.url.pathname.endsWith('/unexpected-arrival'))!.body).toEqual({
      target: { resource_type: 'berth', resource_id: '0190a000-0000-7000-8000-00000000b002' },
      departure: '2026-07-11',
      new_vessel: { name: 'Grecale', loa_cm: 900, beam_cm: 320, hull_type: 'monohull_motor' },
    });
    expect(await screen.findByText('Arrivo registrato')).toBeInTheDocument();
  });

  it('lands the dock role on the panel on a phone', async () => {
    phone();
    const dockMe = makeMe({}, ['marina.read', 'map.view', 'presence.write', 'transits.desk']);
    dockMe.marinas[0]!.roles = ['dock'];
    const { router } = renderApp(`/m/${MARINA_ID}`, [
      ...baseRoutes(dockMe),
      { path: `${API}/movements`, body: movements },
    ]);
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}/dock`));
  });

  it('shows the dock card on the overview for presence.write on a desktop', async () => {
    phone(1280);
    renderApp(`/m/${MARINA_ID}`, [
      ...baseRoutes(meDesk()),
      { path: `${API}/deadlines`, body: { items: [], next_cursor: null } },
    ]);
    const card = await screen.findByTestId('dashboard-dock-card');
    expect(within(card).getByRole('link', { name: 'Apri il pannello' })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/dock`,
    );
  });

  it('warns about a departure with an overdue balance without blocking it', async () => {
    phone();
    const departing = {
      ...movements,
      arrivals_expected: [],
      departures_expected: [
        {
          vessel: vesselBrief,
          contract: { id: 'c2', kind: 'transit', status: 'arrived', code: 'TRN-2026-0002' },
          resource: { type: 'berth', id: 'b7', code: 'A07' },
          allocation_id: ALLOC_ID,
        },
      ],
    };
    renderApp(`/m/${MARINA_ID}/dock`, [
      ...baseRoutes(),
      { path: `${API}/movements`, body: departing },
      {
        path: `${API}/maps/${MAP_ID}/payment-status`,
        body: {
          date: '2026-07-10',
          resources: [
            { resource_type: 'berth', resource_id: 'b7', state: 'overdue', suspended: false },
            { resource_type: 'berth', resource_id: 'b8', state: 'ok', suspended: false },
          ],
        },
      },
    ]);
    const row = await screen.findByTestId(`dock-row-${VESSEL_ID}`);
    expect(within(row).getByTestId('dock-row-payment-warning')).toHaveTextContent(
      'Saldo da verificare',
    );
    expect(within(row).getByText(/A07/)).toBeInTheDocument();
    // Only a warning: the departure stays one tap away.
    expect(within(row).getByRole('button', { name: 'Partito' })).toBeEnabled();
  });

  it('checks photo type and size before upload', () => {
    expect(photoError(new File(['x'], 'a.gif', { type: 'image/gif' }))).toBe(
      'desk:presence.photoType',
    );
    expect(
      photoError(new File([new Uint8Array(8 * 1024 * 1024 + 1)], 'a.jpg', { type: 'image/jpeg' })),
    ).toBe('desk:presence.photoSize');
    expect(photoError(new File(['x'], 'a.webp', { type: 'image/webp' }))).toBeNull();
    expect(photoError(null)).toBeNull();
  });
});
