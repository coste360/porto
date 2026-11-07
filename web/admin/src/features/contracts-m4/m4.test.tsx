import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Schemas } from '@porto/shared/api';

import { MARINA_ID, renderApp } from '../../test/renderApp';
import {
  API,
  CONTRACT_ID,
  CUSTOMER_ID,
  baseRoutes,
  meDesk,
  transit,
  DESK_PERMISSIONS,
} from '../desk/test/fixtures';
import { pickOption } from '../pricing/test/dom';

vi.mock('../operations/public', () => ({
  opsMapSearch: () => ({}),
  PlacementPicker: () => null,
}));
vi.mock('../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-11-07T08:00:00Z'));
});
afterEach(() => vi.useRealTimers());

const M4_PERMISSIONS = [
  ...DESK_PERMISSIONS,
  'payments.collect',
  'audit.read',
] as typeof DESK_PERMISSIONS;
const routes = () => baseRoutes(meDesk(M4_PERMISSIONS));

const contract = transit({
  id: CONTRACT_ID,
  code: 'ORM-2026-0007',
  kind: 'mooring',
  term: 'annual',
  status: 'active',
  starts_on: '2026-01-01',
  ends_on: '2027-01-01',
  total_cents: 300_001,
  customer: { id: CUSTOMER_ID, display_name: 'Mario Rossi' },
  version: 6,
});

const inst = (
  seq: number,
  amount: number,
  over: Partial<Schemas['Installment']> = {},
): Schemas['Installment'] => ({
  id: `i${seq}`,
  contract_id: CONTRACT_ID,
  contract_code: 'ORM-2026-0007',
  customer: { id: CUSTOMER_ID, display_name: 'Mario Rossi' },
  plan_version: 1,
  seq,
  kind: seq === 1 ? 'deposit' : 'installment',
  issue_on: '2026-10-15',
  due_on: '2026-11-01',
  amount_cents: amount,
  status: 'issued',
  overdue: true,
  version: 2,
  ...over,
});

const plan: Schemas['InstallmentPlan'] = {
  contract_id: CONTRACT_ID,
  plan_version: 1,
  total_cents: 300_001,
  sum_cents: 300_001,
  items: [
    inst(1, 90_000, { status: 'paid', overdue: false }),
    inst(2, 105_000),
    inst(3, 105_001, { status: 'scheduled', overdue: false, due_on: '2027-02-01' }),
  ],
};

describe('contract page: instalments tab', () => {
  it('renders the plan from the API with Σ = total and settles one instalment', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/contracts/${CONTRACT_ID}`, [
      ...routes(),
      { path: `${API}/contracts/${CONTRACT_ID}`, body: contract },
      { path: `${API}/contracts/${CONTRACT_ID}/installments`, body: plan },
      {
        method: 'POST',
        path: `${API}/installments/i2/settle`,
        body: { ...inst(2, 105_000), status: 'paid' },
      },
    ]);
    expect(await screen.findByRole('heading', { name: 'ORM-2026-0007' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('tab', { name: 'Rate' }));
    const sum = await screen.findByTestId('plan-sum');
    expect(sum.textContent?.replace(/\s/g, ' ')).toContain('3.000,01');
    expect(screen.getByText(/La somma delle rate è uguale al totale/)).toBeInTheDocument();
    const settleButtons = screen.getAllByRole('button', { name: 'Registra incasso' });
    expect(settleButtons).toHaveLength(2);
    await userEvent.click(settleButtons[0]!);
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Registra' }));
    expect(await within(dialog).findByText('Scegli il metodo di pagamento.')).toBeInTheDocument();
    await pickOption('Metodo', 'Bonifico', dialog);
    await userEvent.click(within(dialog).getByRole('button', { name: 'Registra' }));
    expect(
      await within(dialog).findByText('Per bonifici e assegni serve il riferimento.'),
    ).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText(/Riferimento/), 'CRO 123');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Registra' }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.pathname.endsWith('/i2/settle'))).toBe(true),
    );
    const call = calls.find((c) => c.url.pathname.endsWith('/i2/settle'))!;
    expect(call.headers.get('If-Match')).toBe('"2"');
    expect(call.headers.get('Idempotency-Key')).toBeTruthy();
    expect(call.body).toEqual({
      method: 'bank_transfer',
      reference: 'CRO 123',
      settled_on: '2026-11-07',
    });
  });

  it('flags an unbalanced plan', async () => {
    renderApp(`/m/${MARINA_ID}/contracts/${CONTRACT_ID}`, [
      ...routes(),
      { path: `${API}/contracts/${CONTRACT_ID}`, body: contract },
      {
        path: `${API}/contracts/${CONTRACT_ID}/installments`,
        body: { ...plan, sum_cents: 300_000 },
      },
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Rate' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('non è uguale al totale');
  });

  it('suspends an active contract with a required reason', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/contracts/${CONTRACT_ID}`, [
      ...routes(),
      { path: `${API}/contracts/${CONTRACT_ID}`, body: contract },
      {
        method: 'POST',
        path: `${API}/contracts/${CONTRACT_ID}/suspend`,
        body: { ...contract, status: 'suspended' },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Sospendi' }));
    const dialog = await screen.findByRole('dialog');
    const submit = within(dialog).getByRole('button', { name: 'Sospendi' });
    expect(submit).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText('Motivo'), 'Morosità');
    await userEvent.click(submit);
    await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/suspend'))).toBe(true));
    expect(calls.find((c) => c.url.pathname.endsWith('/suspend'))!.body).toEqual({
      reason: 'Morosità',
    });
  });
});

describe('scadenzario', () => {
  it('shows the ageing and bulk-settles the selection in one call', async () => {
    const page = {
      items: [inst(2, 105_000), inst(4, 50_000, { id: 'i4', due_on: '2026-08-01' })],
      next_cursor: null,
      totals: { count: 2, amount_cents: 155_000 },
    };
    const { calls } = renderApp(`/m/${MARINA_ID}/installments`, [
      ...routes(),
      { path: `${API}/installments`, body: page },
      { method: 'POST', path: `${API}/installments/settle`, body: { settled: 2 } },
    ]);
    expect(await screen.findAllByTestId('installment-row')).toHaveLength(2);
    expect(screen.getByTestId('ageing-d0_30')).toHaveTextContent('1.050,00');
    expect(screen.getByTestId('ageing-d90plus')).toHaveTextContent('500,00');
    const first = calls.find((c) => c.url.pathname === `${API}/installments`)!;
    expect(first.url.searchParams.get('status')).toBe('issued');
    await userEvent.click(screen.getByRole('checkbox', { name: 'Seleziona tutte' }));
    await userEvent.click(screen.getByRole('button', { name: 'Registra incasso' }));
    const dialog = await screen.findByRole('dialog');
    await pickOption('Metodo', 'Contanti', dialog);
    await userEvent.click(within(dialog).getByRole('button', { name: 'Registra' }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.pathname.endsWith('/installments/settle'))).toBe(true),
    );
    expect(calls.find((c) => c.url.pathname.endsWith('/installments/settle'))!.body).toEqual({
      items: [
        { id: 'i2', version: 2 },
        { id: 'i4', version: 2 },
      ],
      method: 'cash',
      settled_on: '2026-11-07',
    });
  });
});

describe('renewal campaign wizard', () => {
  const campaign: Schemas['RenewalCampaign'] = {
    id: 'camp1',
    name: 'Rinnovi 2027',
    filter: { terms: ['annual'] },
    target_starts_rule: 'next_term',
    deadline_on: '2026-12-07',
    status: 'draft',
    message: '',
    counts: {},
    created_at: '2026-11-07T08:00:00Z',
    version: 1,
  };
  const proposal = (
    id: string,
    over: Partial<Schemas['RenewalProposal']> = {},
  ): Schemas['RenewalProposal'] => ({
    id,
    source_contract: { id: `c-${id}`, kind: 'mooring', status: 'active', code: `ORM-${id}` },
    customer: { id: CUSTOMER_ID, display_name: 'Mario Rossi' },
    status: 'previewed',
    old_total_cents: 300_000,
    new_total_cents: 309_000,
    deadline_on: '2026-12-07',
    sent_count: 0,
    created_at: '2026-11-07T08:00:00Z',
    version: 1,
    ...over,
  });

  it('creates the campaign, previews it, then launches', async () => {
    const preview: Schemas['RenewalCampaignPreview'] = {
      campaign: { ...campaign, version: 2 },
      counts: {},
      proposals: {
        items: [
          proposal('p1'),
          proposal('p2', { status: 'excluded', exclusion_reason: 'arrears' }),
          proposal('p3', { status: 'excluded', exclusion_reason: 'berth_unavailable' }),
        ],
        next_cursor: null,
      },
    };
    const { calls } = renderApp(`/m/${MARINA_ID}/renewals/new`, [
      ...routes(),
      { path: `${API}/berth-groups`, body: { items: [] } },
      { path: `${API}/price-lists`, body: { items: [] } },
      { method: 'POST', path: `${API}/renewal-campaigns`, status: 201, body: campaign },
      { method: 'POST', path: `${API}/renewal-campaigns/camp1/preview`, body: preview },
      { path: `${API}/renewal-campaigns/camp1`, body: { ...campaign, version: 2 } },
      {
        method: 'PATCH',
        path: `${API}/renewal-campaigns/camp1`,
        body: { ...campaign, version: 3 },
      },
      {
        method: 'POST',
        path: `${API}/renewal-campaigns/camp1/launch`,
        body: { ...campaign, status: 'sending', version: 4 },
      },
    ]);
    await userEvent.type(await screen.findByLabelText('Nome della campagna'), 'Rinnovi 2027');
    await userEvent.click(screen.getByRole('button', { name: 'Calcola anteprima' }));
    expect(await screen.findByText('Scegli almeno un criterio.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: 'Annuale' }));
    await userEvent.click(screen.getByRole('button', { name: 'Calcola anteprima' }));
    expect(await screen.findAllByTestId('proposal-row')).toHaveLength(3);
    expect(
      calls.find((c) => c.method === 'POST' && c.url.pathname === `${API}/renewal-campaigns`)!.body,
    ).toEqual({
      name: 'Rinnovi 2027',
      filter: { terms: ['annual'] },
      deadline_on: '2026-12-07',
    });
    // Excluded for arrears can be included; berth_unavailable cannot.
    expect(screen.getAllByRole('button', { name: 'Includi' })).toHaveLength(1);
    expect(screen.getByTestId('preview-totals')).toHaveTextContent('3.090,00');
    await userEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    expect(await screen.findByLabelText('Messaggio ai clienti')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Avvia la campagna' }));
    await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/launch'))).toBe(true));
    expect(calls.find((c) => c.url.pathname.endsWith('/launch'))!.headers.get('If-Match')).toBe(
      '"3"',
    );
  });

  it('shows the monitor counters and filters proposals by state', async () => {
    const open = {
      ...campaign,
      status: 'open' as const,
      counts: { sent: 12, accepted: 3, completed: 2 },
    };
    const { calls } = renderApp(`/m/${MARINA_ID}/renewals/camp1`, [
      ...routes(),
      { path: `${API}/renewal-campaigns/camp1`, body: open },
      {
        path: `${API}/renewal-campaigns/camp1/proposals`,
        body: {
          items: [proposal('p1', { status: 'sent' }), proposal('p2', { status: 'completed' })],
          next_cursor: null,
        },
      },
    ]);
    expect(await screen.findByTestId('count-sent')).toHaveTextContent('12');
    expect(screen.getByTestId('count-accepted')).toHaveTextContent('3');
    const rows = await screen.findAllByTestId('proposal-row');
    expect(
      within(rows[0]!).getByRole('button', { name: 'Accetta per il cliente' }),
    ).toBeInTheDocument();
    expect(within(rows[1]!).queryByRole('button')).toBeNull();
    await userEvent.click(screen.getByTestId('count-sent'));
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.url.pathname.endsWith('/proposals') && c.url.searchParams.get('status') === 'sent',
        ),
      ).toBe(true),
    );
  });
});
