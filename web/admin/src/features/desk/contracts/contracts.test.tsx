import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { pickOption } from '../../pricing/test/dom';
import { contractBody, type ContractDraft } from '../lib/contract';
import {
  API,
  B1,
  CONTRACT_ID,
  CUSTOMER_ID,
  VESSEL_ID,
  baseRoutes,
  transit,
} from '../test/fixtures';

vi.mock('../../operations/public', () => ({
  opsMapSearch: (p: { mapId?: string; focus?: string }) => ({
    ...(p.mapId ? { map: p.mapId } : {}),
    ...(p.focus ? { focus: p.focus } : {}),
  }),
  PlacementPicker: () => null,
}));
vi.mock('../../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../../maps/routes', () => ({ mapRoutes: () => [] }));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-07-10T08:00:00Z'));
});
afterEach(() => vi.useRealTimers());

const draft: ContractDraft = {
  term: 'annual',
  customerId: CUSTOMER_ID,
  vesselId: VESSEL_ID,
  startsOn: '2026-04-01',
  endsOn: null,
  target: { resource_type: 'berth', resource_id: B1 },
  extraLandIds: ['p1', 'p1'],
  notes: '',
  activate: true,
  fitOverrideReason: null,
};

const mooring = transit({
  id: CONTRACT_ID,
  code: 'ORM-2026-0001',
  kind: 'mooring',
  term: 'annual',
  status: 'active',
  starts_on: '2026-01-01',
  ends_on: '2027-01-01',
  customer: { id: CUSTOMER_ID, display_name: 'Mario Rossi' },
  allocations: [
    {
      id: 'al1',
      resource_type: 'berth',
      resource_id: B1,
      resource_code: 'A01',
      map_id: 'm1',
      first_night: '2026-01-01',
      last_night: '2026-12-31',
      kind: 'contract',
      status: 'active',
      version: 1,
      released: [],
    },
  ],
});

describe('contract wizard defaults', () => {
  it('defaults the end per term and covers the stay with the placement', () => {
    const r = contractBody(draft);
    expect(r.ok && r.body).toMatchObject({
      term: 'annual',
      starts_on: '2026-04-01',
      ends_on: '2027-04-01',
      placements: [
        {
          resource_type: 'berth',
          resource_id: B1,
          first_night: '2026-04-01',
          last_night: '2027-03-31',
        },
      ],
      extra_resources: [{ land_resource_id: 'p1' }],
      activate: true,
    });
    const monthly = contractBody({ ...draft, term: 'monthly', startsOn: '2026-01-31' });
    expect(monthly.ok && monthly.body.ends_on).toBe('2026-02-28');
    expect(contractBody({ ...draft, term: 'seasonal' })).toEqual({
      ok: false,
      error: 'desk:contract.errors.dates',
    });
    expect(contractBody({ ...draft, term: 'period', endsOn: '2026-06-01' }).ok).toBe(true);
    expect(contractBody({ ...draft, target: null })).toEqual({
      ok: false,
      error: 'desk:contract.errors.placement',
    });
  });
});

describe('contract pages', () => {
  it('creates an active annual contract from the wizard', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/contracts/new?vessel=${VESSEL_ID}&rt=berth&rid=${B1}&from=2026-04-01`,
      [
        ...baseRoutes(),
        { method: 'POST', path: `${API}/contracts`, status: 201, body: mooring },
        { path: `${API}/contracts/${CONTRACT_ID}`, body: mooring },
      ],
    );
    const wizard = await screen.findByTestId('desk-contract-wizard');
    await pickOption('Cliente', 'Mario Rossi', wizard);
    await userEvent.click(
      within(wizard).getByRole('button', { name: 'Attiva (firmato su carta)' }),
    );
    await waitFor(() =>
      expect(calls.some((c) => c.method === 'POST' && c.url.pathname === `${API}/contracts`)).toBe(
        true,
      ),
    );
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({
      term: 'annual',
      customer_id: CUSTOMER_ID,
      vessel_id: VESSEL_ID,
      starts_on: '2026-04-01',
      ends_on: '2027-04-01',
      placements: [
        {
          resource_type: 'berth',
          resource_id: B1,
          first_night: '2026-04-01',
          last_night: '2027-03-31',
        },
      ],
      activate: true,
    });
    expect(await screen.findByRole('heading', { name: 'ORM-2026-0001' })).toBeInTheDocument();
  });

  it('terminates an active contract and links the map', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/contracts/${CONTRACT_ID}`, [
      ...baseRoutes(),
      { path: `${API}/contracts/${CONTRACT_ID}`, body: mooring },
      {
        method: 'POST',
        path: `${API}/contracts/${CONTRACT_ID}/terminate`,
        body: { ...mooring, status: 'terminated' },
      },
    ]);
    expect(await screen.findByRole('heading', { name: 'ORM-2026-0001' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Mostra sulla mappa' }).getAttribute('href')).toContain(
      `focus=${B1}`,
    );
    expect(screen.queryByRole('button', { name: 'Attiva (firmato su carta)' })).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Cessa il contratto' }));
    const dialog = await screen.findByRole('dialog');
    const submit = within(dialog).getByRole('button', { name: 'Cessa il contratto' });
    expect(submit).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText('Motivo'), 'Vendita della barca');
    await userEvent.click(submit);
    await waitFor(() =>
      expect(calls.some((c) => c.url.pathname.endsWith('/terminate'))).toBe(true),
    );
    const call = calls.find((c) => c.url.pathname.endsWith('/terminate'))!;
    expect(call.headers.get('If-Match')).toBe('"4"');
    expect(call.body).toEqual({ effective_on: '2026-07-10', reason: 'Vendita della barca' });
  });
});
