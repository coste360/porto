import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type * as Offline from '@porto/shared/offline';
import type { NewOp, QueuedOp } from '@porto/shared/offline';
import { renderWithProviders } from '@porto/shared/testing';

import { renderApp } from '../../test/renderApp';
import { makeDashboard, productionTile, yardTile } from '../dashboard/test/fixtures';
import { baseRoutes, mapDoc } from '../desk/test/fixtures';
import { TransitQrButton } from './components/TransitQrButton';
import { setPreferFullVersion } from './lib/fullVersion';
import {
  ALLOC_ID,
  API,
  B1,
  CHARGE_ID,
  MAP_ID,
  MARINA_ID,
  REGISTER_ID,
  TRANSIT_ID,
  cashSession,
  collectible,
  dayView,
  meDock,
  movements,
  vesselBrief,
} from './test/fixtures';

// The offline queue is another builder's module: the phone screens use only its public API, faked here.
const q = vi.hoisted(() => ({
  online: true,
  ops: [] as QueuedOp[],
  enqueued: [] as NewOp[],
  n: 0,
}));
vi.mock('@porto/shared/offline', async (importOriginal) => {
  const real = await importOriginal<typeof Offline>();
  return {
    ...real,
    enqueue: async (op: NewOp) => {
      q.enqueued.push(op);
      q.n += 1;
      return {
        ...op,
        id: `op-${q.n}`,
        key: `key-${q.n}`,
        createdAt: 'x',
        state: 'pending',
        attempts: 0,
      };
    },
    useQueue: () => ({
      ops: q.ops,
      pending: q.ops.length,
      conflicts: q.ops.filter((o) => o.state === 'conflict').length,
      sendNow: () => {},
      discard: async () => {},
      retryAsNew: async () => {},
    }),
    useOnlineStatus: () => ({ online: q.online, swActive: false }),
    assertOnline: (id: string, reason?: 'allocation' | 'document' | 'online_only') => {
      if (!q.online) throw new real.OfflineForbiddenError(id, reason);
    },
    purgeOfflineData: async () => {},
  };
});
vi.mock('../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

function phone(width = 360) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width });
}

// jsdom lacks pointer capture, used by the drawer (vaul) on pointer down.
if (!HTMLElement.prototype.setPointerCapture) {
  HTMLElement.prototype.setPointerCapture = () => {};
  HTMLElement.prototype.releasePointerCapture = () => {};
}

let innerWidth = 1024;
beforeEach(() => {
  innerWidth = window.innerWidth;
  q.online = true;
  q.ops = [];
  q.enqueued = [];
  q.n = 0;
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-07-10T08:00:00Z'));
  setPreferFullVersion(false);
});
afterEach(() => {
  phone(innerWidth);
  vi.useRealTimers();
});

const dockRoutes = (extra: Parameters<typeof renderApp>[1] = []) => [
  ...extra,
  {
    path: `${API}/maps/${MAP_ID}`,
    body: {
      ...mapDoc,
      width_cm: 60000,
      height_cm: 40000,
      grid_cm: 500,
      water_polygons: [],
      land_polygons: [],
    },
  },
  ...baseRoutes(meDock()),
  { path: `${API}/movements`, body: movements },
  { path: `${API}/maps/${MAP_ID}/day`, body: dayView },
  { path: `${API}/maps/${MAP_ID}/payment-status`, body: { date: '2026-07-10', resources: [] } },
  { path: `${API}/collectibles`, body: { items: [], next_cursor: null } },
  { path: `${API}/cash-sessions/current`, body: cashSession },
];

const queuedArrival = (): QueuedOp => ({
  id: 'op-a',
  kind: 'presence.record',
  key: 'k',
  userId: 'u1',
  marinaId: MARINA_ID,
  createdAt: '2026-07-10T07:59:00Z',
  lane: `vessel:${vesselBrief.id}`,
  request: {
    operationId: 'recordPresence',
    path: `${API}/presences`,
    body: { vessel_id: vesselBrief.id, event: 'arrived' },
  },
  state: 'pending',
  attempts: 0,
  label: { key: 'offline:queue.label.arrived', params: { vessel: vesselBrief.name } },
});

describe('dock phone screen', () => {
  it('works at 360 px: phone shell, five bottom tabs, arrival with a note queued with the berth seen', async () => {
    phone();
    renderApp(`/m/${MARINA_ID}/dock`, dockRoutes());
    const page = await screen.findByTestId('dock-page');
    expect(screen.getByTestId('phone-shell')).toBeInTheDocument();
    expect(screen.getByTestId('phone-marina')).toHaveTextContent('Marina Prova');
    for (const name of ['Oggi', 'Mappa', 'Scansiona', 'Cassa', 'Altro'])
      expect(within(page).getByRole('tab', { name })).toHaveClass('min-h-11');
    const row = await screen.findByTestId(`dock-row-${vesselBrief.id}`);
    expect(within(row).getByText(/A01 · 11,34 m × 3,80 m · 14:00/)).toBeInTheDocument();
    const tap = within(row).getByRole('button', { name: 'Arrivato' });
    expect(tap).toHaveClass('min-h-11');
    await userEvent.click(tap);
    const sheet = await screen.findByTestId('desk-presence-dialog');
    await userEvent.type(within(sheet).getByLabelText('Note'), 'Ormeggiato di poppa');
    await userEvent.click(within(sheet).getByRole('button', { name: "Registra l'arrivo" }));
    await waitFor(() => expect(q.enqueued).toHaveLength(1));
    expect(q.enqueued[0]).toEqual({
      kind: 'presence.record',
      userId: 'u1',
      marinaId: MARINA_ID,
      lane: `vessel:${vesselBrief.id}`,
      request: {
        operationId: 'recordPresence',
        path: `${API}/presences`,
        body: {
          vessel_id: vesselBrief.id,
          event: 'arrived',
          at: '2026-07-10T08:00:00.000Z',
          allocation_id: ALLOC_ID,
          expected_resource_id: B1,
          notes: 'Ormeggiato di poppa',
        },
      },
      label: { key: 'offline:queue.label.arrived', params: { vessel: 'Libeccio' } },
    });
  });

  it('queues the photo after its presence, bound to the presence id', async () => {
    phone();
    renderApp(`/m/${MARINA_ID}/dock`, dockRoutes());
    const row = await screen.findByTestId(`dock-row-${vesselBrief.id}`);
    await userEvent.click(within(row).getByRole('button', { name: 'Arrivato' }));
    const sheet = await screen.findByTestId('desk-presence-dialog');
    const photo = new File(['jpeg'], 'boat.jpg', { type: 'image/jpeg' });
    fireEvent.change(within(sheet).getByLabelText('Foto'), { target: { files: [photo] } });
    await userEvent.click(within(sheet).getByRole('button', { name: "Registra l'arrivo" }));
    await waitFor(() => expect(q.enqueued).toHaveLength(2));
    expect(q.enqueued[1]).toMatchObject({
      kind: 'presence.photo',
      lane: `vessel:${vesselBrief.id}`,
      dependsOn: { opId: 'op-1', bind: { presenceId: 'presence.id' } },
      request: {
        operationId: 'uploadPresencePhoto',
        path: `${API}/presences/{presenceId}/photo`,
        contentType: 'image/jpeg',
      },
    });
    expect(q.enqueued[1]!.blob).toBeInstanceOf(Blob);
  });

  it('overlays a queued arrival: "In coda" and the departure as the next action', async () => {
    phone();
    q.ops = [queuedArrival()];
    renderApp(`/m/${MARINA_ID}/dock`, dockRoutes());
    const row = await screen.findByTestId(`dock-row-${vesselBrief.id}`);
    expect(within(row).getByTestId('dock-row-queued')).toHaveTextContent('In coda');
    expect(within(row).getByRole('button', { name: 'Partito' })).toBeInTheDocument();
    expect(within(row).queryByRole('button', { name: 'Arrivato' })).not.toBeInTheDocument();
    expect(screen.getByTestId('offline-pending')).toHaveTextContent('1');
  });

  it('offline: online-only controls are disabled with the reason inline, data shows its age', async () => {
    phone();
    q.online = false;
    renderApp(`/m/${MARINA_ID}/dock`, dockRoutes());
    expect(await screen.findByTestId('stale-since')).toHaveTextContent(/Dati aggiornati alle/);
    expect(screen.getByTestId('offline-indicator')).toHaveAttribute('data-online', 'false');
    await userEvent.click(screen.getByRole('tab', { name: 'Altro' }));
    const more = await screen.findByTestId('dock-more');
    for (const id of ['dock-more-unexpected', 'dock-more-measure', 'dock-more-releases'])
      expect(within(more).getByTestId(id)).toBeDisabled();
    const reasons = within(more).getAllByTestId('offline-reason');
    expect(reasons.map((r) => r.getAttribute('data-reason'))).toEqual([
      'allocation',
      'online_only',
      'allocation',
    ]);
    expect(reasons[0]).toHaveTextContent(
      'Non disponibile offline: richiede la garanzia di disponibilità del server.',
    );
    // Arrivals with an allocation stay available offline (queued).
    await userEvent.click(screen.getByRole('tab', { name: 'Oggi' }));
    const row = await screen.findByTestId(`dock-row-${vesselBrief.id}`);
    expect(within(row).getByRole('button', { name: 'Arrivato' })).toBeEnabled();
  });

  it('cash: collects an invoice-mode charge through the queue, amount prefilled with the residual', async () => {
    phone();
    renderApp(`/m/${MARINA_ID}/dock?tab=cash`, [
      { path: `${API}/collectibles`, body: { items: [collectible()], next_cursor: null } },
      ...dockRoutes(),
    ]);
    expect(await screen.findByText(/Cassa aperta dal 10\/07\/2026 · CASSA1/)).toBeInTheDocument();
    const item = await screen.findByTestId(`dock-collectible-${CHARGE_ID}`);
    expect(item).toHaveTextContent('45,00');
    await userEvent.click(within(item).getByRole('button', { name: 'Incassa' }));
    const sheet = await screen.findByTestId('dock-collect-sheet');
    const amount = within(sheet).getByLabelText('Importo incassato in contanti');
    expect(amount).toHaveValue('45,00');
    await userEvent.clear(amount);
    await userEvent.type(amount, '50');
    expect(within(sheet).getByRole('button', { name: "Registra l'incasso" })).toBeDisabled();
    await userEvent.clear(amount);
    await userEvent.type(amount, '30');
    await userEvent.click(within(sheet).getByRole('button', { name: "Registra l'incasso" }));
    await waitFor(() => expect(q.enqueued).toHaveLength(1));
    expect(q.enqueued[0]).toMatchObject({
      kind: 'payment.cash',
      lane: `charge:${CHARGE_ID}`,
      request: {
        operationId: 'recordPayment',
        path: `${API}/payments`,
        body: {
          kind: 'payment',
          method: 'cash',
          amount_cents: 3000,
          received_on: '2026-07-10',
          cash_register_id: REGISTER_ID,
          matches: [{ charge_id: CHARGE_ID, amount_cents: 3000 }],
        },
      },
    });
  });

  it('cash: a receipt-mode charge is refused offline with the reason', async () => {
    phone();
    q.online = false;
    renderApp(`/m/${MARINA_ID}/dock?tab=cash`, [
      {
        path: `${API}/collectibles`,
        body: { items: [collectible({ fiscal_mode: 'receipt' })], next_cursor: null },
      },
      ...dockRoutes(),
    ]);
    const item = await screen.findByTestId(`dock-collectible-${CHARGE_ID}`);
    expect(within(item).getByRole('button', { name: 'Incassa' })).toBeDisabled();
    expect(within(item).getByTestId('offline-reason')).toHaveTextContent(
      'Non disponibile offline: emette un documento.',
    );
  });

  it('cash: a receipt-mode charge online is sent directly (never queued) and offers the receipt', async () => {
    phone();
    const { calls } = renderApp(`/m/${MARINA_ID}/dock?tab=cash`, [
      {
        path: `${API}/collectibles`,
        body: { items: [collectible({ fiscal_mode: 'receipt' })], next_cursor: null },
      },
      { method: 'POST', path: `${API}/payments`, status: 201, body: { id: 'pay-1' } },
      ...dockRoutes(),
    ]);
    const item = await screen.findByTestId(`dock-collectible-${CHARGE_ID}`);
    await userEvent.click(within(item).getByRole('button', { name: 'Incassa' }));
    const sheet = await screen.findByTestId('dock-collect-sheet');
    await userEvent.click(within(sheet).getByRole('button', { name: "Registra l'incasso" }));
    const link = await screen.findByRole('link', { name: 'Ricevuta' });
    expect(link).toHaveAttribute('href', `${API}/payments/pay-1/receipt?format=80`);
    expect(q.enqueued).toHaveLength(0);
    const post = calls.find((c) => c.method === 'POST' && c.url.pathname.endsWith('/payments'))!;
    expect(post.headers.get('Idempotency-Key')).toBeTruthy();
    expect(post.headers.get('Porto-Offline-Queued-At')).toBeNull();
    expect(post.body).toMatchObject({ method: 'cash', amount_cents: 4500 });
  });

  it('cash: without an open session nothing can be collected', async () => {
    phone();
    renderApp(`/m/${MARINA_ID}/dock?tab=cash`, [
      { path: `${API}/collectibles`, body: { items: [collectible()], next_cursor: null } },
      {
        path: `${API}/cash-sessions/current`,
        status: 404,
        body: { type: 'session_not_open', title: 'x', status: 404 },
      },
      ...dockRoutes(),
    ]);
    expect(await screen.findByText(/Nessuna cassa aperta/)).toBeInTheDocument();
    const item = await screen.findByTestId(`dock-collectible-${CHARGE_ID}`);
    expect(within(item).getByRole('button', { name: 'Incassa' })).toBeDisabled();
  });

  const wedge = (text: string) => {
    let at = 1000;
    for (const ch of text) {
      fireEvent.keyDown(document.body, { key: ch, timeStamp: at });
      at += 5;
    }
    fireEvent.keyDown(document.body, { key: 'Enter', timeStamp: at });
  };

  it('scan: a transit QR from a USB reader opens the card from the day data, Arrivato works', async () => {
    phone();
    renderApp(`/m/${MARINA_ID}/dock?tab=scan`, dockRoutes());
    await screen.findByTestId('dock-scan');
    expect(screen.getByText('Scegli una foto del QR')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId('phone-marina')).toHaveTextContent('Marina Prova'),
    );
    wedge(`PT1:${TRANSIT_ID.toUpperCase()}`);
    const card = await screen.findByTestId('dock-scan-card');
    expect(card).toHaveTextContent('Transito TRN-2026-0001');
    expect(card).toHaveTextContent('Libeccio');
    expect(card).toHaveTextContent('A01');
    await userEvent.click(within(card).getByRole('button', { name: 'Arrivato' }));
    await screen.findByTestId('desk-presence-dialog');
  });

  it('scan offline: an id not in the day data is "non trovato nei dati del giorno"; a bad QR is refused', async () => {
    phone();
    q.online = false;
    renderApp(`/m/${MARINA_ID}/dock?tab=scan`, dockRoutes());
    await screen.findByTestId('dock-scan');
    await waitFor(() =>
      expect(screen.getByTestId('phone-marina')).toHaveTextContent('Marina Prova'),
    );
    wedge('NOT-A-TRANSIT-QR');
    expect(await screen.findByRole('alert')).toHaveTextContent('QR non riconosciuto');
    wedge('PT1:0190a000-0000-7000-8000-00000000ffff');
    expect(await screen.findByTestId('dock-scan-not-found')).toHaveTextContent(
      'Non trovato nei dati del giorno',
    );
    expect(screen.queryByTestId('dock-scan-card')).not.toBeInTheDocument();
  });

  it('map: day map in schema mode with contexts and legend counts', async () => {
    phone();
    renderApp(`/m/${MARINA_ID}/dock?tab=map`, dockRoutes());
    const canvas = await screen.findByTestId('phone-map-canvas');
    expect(canvas).toHaveAttribute('data-context', 'presence');
    await waitFor(() =>
      expect(screen.getByTestId('phone-map-legend-arrival')).toHaveTextContent('In arrivo oggi1'),
    );
    await userEvent.click(screen.getByTestId('phone-map-context-movements'));
    expect(canvas).toHaveAttribute('data-context', 'movements');
  });

  it('keeps the tab in the search and the M3 test id', async () => {
    phone();
    const { router } = renderApp(`/m/${MARINA_ID}/dock`, dockRoutes());
    await userEvent.click(await screen.findByRole('tab', { name: 'Cassa' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: 'cash' }));
    expect(screen.getByTestId('dock-page')).toHaveAttribute('data-tab', 'cash');
  });
});

describe('direction phone screen', () => {
  const directionMe = () =>
    meDock(
      ['marina.read', 'map.view', 'customers.read', 'reports.all', 'contracts.read'],
      'accounting',
    );

  it('dashboard tiles with today counts, deadlines grouped by week', async () => {
    phone();
    const { calls } = renderApp(`/m/${MARINA_ID}/direction`, [
      ...baseRoutes(directionMe()),
      { path: `${API}/movements`, body: movements },
      { path: `${API}/maps/${MAP_ID}/day`, body: dayView },
      {
        path: `${API}/dashboard`,
        body: makeDashboard([productionTile, yardTile], {
          period: { from: '2026-07-01', to: '2026-07-10' },
        }),
      },
      {
        path: `${API}/deadlines`,
        body: {
          items: [
            {
              id: 'd1',
              kind: 'insurance',
              source_type: 'vessel_document',
              source_id: 's1',
              customer: null,
              vessel: { id: vesselBrief.id, name: 'Libeccio' },
              document_type: null,
              due_on: '2026-07-21',
              status: 'open',
              urgency: 'warn',
              note: '',
              version: 1,
            },
          ],
          next_cursor: null,
        },
      },
    ]);
    const page = await screen.findByTestId('direction-page');
    for (const name of ['Cruscotto', 'Mappa', 'Scadenze'])
      expect(within(page).getByRole('tab', { name })).toHaveClass('min-h-11');
    // Month to date by default; only available tiles, compact, opening the report with the same period.
    const tile = await screen.findByTestId('dashboard-tile');
    expect(tile).toHaveAttribute('data-tile', 'production');
    expect(screen.queryByTestId('tile-sparkline')).not.toBeInTheDocument();
    expect(within(tile).getByTestId('tile-value')).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/reports/production?from=2026-07-01&to=2026-07-10`,
    );
    const dash = calls.find((c) => c.url.pathname.endsWith('/dashboard'))!.url.searchParams;
    expect(dash.get('from')).toMatch(/^\d{4}-\d{2}-01$/);
    await waitFor(() =>
      expect(screen.getAllByTestId('direction-count').map((c) => c.textContent)).toEqual([
        'Arrivi1',
        'Partenze0',
        'In porto0',
      ]),
    );
    await userEvent.click(screen.getByRole('tab', { name: 'Scadenze' }));
    const week = await screen.findByTestId('direction-week');
    expect(week).toHaveTextContent('Settimana dal 20/07/2026');
    expect(week).toHaveTextContent('Assicurazione · 21/07/2026');
    const q = calls.find((c) => c.url.pathname.endsWith('/deadlines'))!.url.searchParams;
    expect(q.get('due_before')).toBe('2026-08-10');
    expect(q.get('status')).toBe('open');
  });

  it('is refused to the dock role', async () => {
    phone();
    renderApp(`/m/${MARINA_ID}/direction`, [...dockRoutes()]);
    await waitFor(() => expect(screen.queryByTestId('direction-page')).not.toBeInTheDocument());
  });
});

describe('phone landing from the overview', () => {
  it('sends the dock role to the dock and reports.all to the direction screen on a phone', async () => {
    phone();
    const a = renderApp(`/m/${MARINA_ID}`, dockRoutes());
    await waitFor(() => expect(a.router.state.location.pathname).toBe(`/m/${MARINA_ID}/dock`));
    a.unmount();
    const b = renderApp(`/m/${MARINA_ID}`, [
      ...baseRoutes(meDock(['marina.read', 'map.view', 'reports.all'], 'viewer')),
      { path: `${API}/movements`, body: movements },
    ]);
    await waitFor(() => expect(b.router.state.location.pathname).toBe(`/m/${MARINA_ID}/direction`));
  });

  it('keeps the overview after "Versione completa"', async () => {
    phone();
    const { router } = renderApp(`/m/${MARINA_ID}/dock`, dockRoutes());
    await userEvent.click(await screen.findByTestId('phone-full-version'));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}`));
    expect(await screen.findByTestId('dashboard-dock-card')).toBeInTheDocument();
  });

  it('keeps the overview on a desktop', async () => {
    phone(1280);
    const { router } = renderApp(`/m/${MARINA_ID}`, dockRoutes());
    expect(await screen.findByTestId('dashboard-dock-card')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}`);
  });
});

describe('transit QR button', () => {
  it('shows the SVG of the transit QR, for transits only', async () => {
    const { unmount } = renderWithProviders(
      <TransitQrButton
        marinaId={MARINA_ID}
        transit={{ id: TRANSIT_ID, kind: 'transit', code: 'TRN-2026-0001' }}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: "QR per l'ormeggiatore" }));
    expect(await screen.findByTestId('transit-qr-image')).toHaveAttribute(
      'src',
      `${API}/transits/${TRANSIT_ID}/qr`,
    );
    unmount();
    renderWithProviders(
      <TransitQrButton
        marinaId={MARINA_ID}
        transit={{ id: TRANSIT_ID, kind: 'mooring', code: 'ORM-2026-0001' }}
      />,
    );
    expect(screen.queryByRole('button', { name: "QR per l'ormeggiatore" })).not.toBeInTheDocument();
  });
});
