import { describe, expect, it } from 'vitest';

import type { Schemas } from '@porto/shared/api';

import {
  campaignCounts,
  campaignInput,
  canInclude,
  cleanFilter,
  includedCount,
  initialWizard,
  previewTotals,
  proposalActions,
  stepError,
  wizardReducer,
  type WizardState,
} from './campaign';
import {
  ageing,
  ageingBucket,
  bulkActions,
  bulkSettleBody,
  liveSum,
  planBalanced,
  settleBody,
} from './installments';

const TODAY = '2026-11-07';

const proposal = (
  id: string,
  over: Partial<Schemas['RenewalProposal']> = {},
): Schemas['RenewalProposal'] => ({
  id,
  source_contract: { id: `c-${id}`, kind: 'mooring', status: 'active', code: `ORM-${id}` } as never,
  status: 'previewed',
  old_total_cents: 300_000,
  new_total_cents: 315_000,
  deadline_on: '2026-12-07',
  sent_count: 0,
  created_at: '2026-11-07T08:00:00Z',
  version: 1,
  ...over,
});

const campaign = (over: Partial<Schemas['RenewalCampaign']> = {}): Schemas['RenewalCampaign'] => ({
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
  ...over,
});

const preview = (items: Schemas['RenewalProposal'][]): Schemas['RenewalCampaignPreview'] => ({
  campaign: campaign({ version: 2 }),
  counts: {},
  proposals: { items, next_cursor: null },
});

describe('campaign wizard reducer', () => {
  it('walks filter → preview → message → launch with validation at each step', () => {
    let s: WizardState = initialWizard('2026-12-07');
    expect(s.step).toBe('filter');
    expect(stepError(s, TODAY)).toBe('name');
    s = wizardReducer(s, { type: 'name', value: '  Rinnovi 2027 ' });
    expect(stepError(s, TODAY)).toBe('filter');
    s = wizardReducer(s, { type: 'filter', patch: { terms: ['annual'], groups: [] } });
    expect(stepError(s, TODAY)).toBeNull();
    expect(campaignInput(s)).toEqual({
      name: 'Rinnovi 2027',
      filter: { terms: ['annual'] },
      deadline_on: '2026-12-07',
    });

    // The preview result moves the wizard to step 2 and stores the campaign version.
    s = wizardReducer(s, {
      type: 'previewed',
      preview: preview([
        proposal('p1'),
        proposal('p2', { status: 'excluded', exclusion_reason: 'arrears' }),
      ]),
    });
    expect(s.step).toBe('preview');
    expect(s.campaign?.version).toBe(2);
    expect(stepError(s, TODAY)).toBeNull();

    s = wizardReducer(s, { type: 'next' });
    expect(s.step).toBe('message');
    s = wizardReducer(s, { type: 'deadline', value: TODAY });
    expect(stepError(s, TODAY)).toBe('deadline');
    s = wizardReducer(s, { type: 'deadline', value: '2026-12-10' });
    expect(stepError(s, TODAY)).toBeNull();
    s = wizardReducer(s, { type: 'next' });
    expect(s.step).toBe('launch');
    expect(stepError(s, TODAY)).toBeNull();
    s = wizardReducer(s, { type: 'saved', campaign: campaign({ status: 'sending' }) });
    expect(stepError(s, TODAY)).toBe('launched');
    expect(wizardReducer(s, { type: 'next' }).step).toBe('launch');
  });

  it('marks the preview stale when the filter changes and refuses forward jumps', () => {
    let s = initialWizard('2026-12-07');
    s = wizardReducer(s, { type: 'name', value: 'X' });
    s = wizardReducer(s, { type: 'filter', patch: { terms: ['annual'] } });
    s = wizardReducer(s, { type: 'previewed', preview: preview([proposal('p1')]) });
    s = wizardReducer(s, { type: 'goto', step: 'filter' });
    expect(s.step).toBe('filter');
    s = wizardReducer(s, { type: 'filter', patch: { terms: ['seasonal'] } });
    expect(s.stale).toBe(true);
    expect(wizardReducer(s, { type: 'goto', step: 'launch' }).step).toBe('filter');
    s = wizardReducer(s, { type: 'next' });
    expect(stepError(s, TODAY)).toBe('preview');
  });

  it('refuses a preview where every proposal is excluded', () => {
    let s = initialWizard('2026-12-07');
    s = wizardReducer(s, {
      type: 'previewed',
      preview: preview([proposal('p1', { status: 'excluded', exclusion_reason: 'no_price' })]),
    });
    expect(stepError(s, TODAY)).toBe('empty');
    s = wizardReducer(s, {
      type: 'proposalChanged',
      proposal: proposal('p1', { status: 'previewed', included_reason: 'ok' }),
    });
    expect(stepError(s, TODAY)).toBeNull();
  });

  it('cleans the filter and totals the preview without the excluded rows', () => {
    expect(cleanFilter({ groups: [], terms: ['annual'], ends_from: '' })).toEqual({
      terms: ['annual'],
    });
    const items = [
      proposal('a'),
      proposal('b', { old_total_cents: 100_001, new_total_cents: 99_999 }),
      proposal('c', { status: 'excluded', exclusion_reason: 'arrears' }),
    ];
    expect(previewTotals(items)).toEqual({
      oldCents: 400_001,
      newCents: 414_999,
      deltaCents: 14_998,
      included: 2,
      excluded: 1,
    });
    expect(includedCount(items)).toBe(2);
  });
});

describe('proposals table states', () => {
  it('offers actions by state and never includes a berth_unavailable proposal', () => {
    expect(proposalActions({ status: 'sent' })).toEqual({
      resend: true,
      accept: true,
      decline: true,
      include: false,
    });
    expect(proposalActions({ status: 'completed' })).toEqual({
      resend: false,
      accept: false,
      decline: false,
      include: false,
    });
    expect(canInclude({ status: 'excluded', exclusion_reason: 'arrears' })).toBe(true);
    expect(canInclude({ status: 'excluded', exclusion_reason: 'berth_unavailable' })).toBe(false);
  });

  it('reads the counters in state order and drops zeros and garbage', () => {
    expect(campaignCounts({ sent: 12, accepted: 3, declined: 0, bogus: 9, expired: 'x' })).toEqual({
      sent: 12,
      accepted: 3,
    });
  });
});

describe('settle dialog mapping', () => {
  it('requires a method and a reference for transfers and cheques', () => {
    expect(settleBody({ method: '', reference: '', settledOn: null }, TODAY)).toEqual({
      ok: false,
      error: 'method',
    });
    expect(settleBody({ method: 'bank_transfer', reference: ' ', settledOn: null }, TODAY)).toEqual(
      {
        ok: false,
        error: 'reference',
      },
    );
    expect(settleBody({ method: 'cash', reference: '', settledOn: null }, TODAY)).toEqual({
      ok: true,
      body: { method: 'cash', settled_on: TODAY },
    });
    expect(
      settleBody({ method: 'cheque', reference: ' A-123 ', settledOn: '2026-11-01' }, TODAY),
    ).toEqual({
      ok: true,
      body: { method: 'cheque', reference: 'A-123', settled_on: '2026-11-01' },
    });
  });

  it('refuses future dates and maps bulk selections with their versions', () => {
    expect(settleBody({ method: 'cash', reference: '', settledOn: '2026-11-08' }, TODAY)).toEqual({
      ok: false,
      error: 'date',
    });
    expect(bulkSettleBody([], { method: 'cash', reference: '', settledOn: null }, TODAY)).toEqual({
      ok: false,
      error: 'empty',
    });
    const many = Array.from({ length: 501 }, (_, i) => ({ id: `i${i}`, version: 1 }));
    expect(bulkSettleBody(many, { method: 'cash', reference: '', settledOn: null }, TODAY)).toEqual(
      {
        ok: false,
        error: 'too_many',
      },
    );
    expect(
      bulkSettleBody(
        [{ id: 'a', version: 2, extra: 1 } as { id: string; version: number }],
        { method: 'card_pos', reference: 'POS 42', settledOn: null },
        TODAY,
      ),
    ).toEqual({
      ok: true,
      body: {
        items: [{ id: 'a', version: 2 }],
        method: 'card_pos',
        reference: 'POS 42',
        settled_on: TODAY,
      },
    });
  });
});

describe('instalments helpers', () => {
  it('buckets overdue instalments by age in integer cents', () => {
    expect(ageingBucket('2026-11-07', TODAY)).toBeNull();
    expect(ageingBucket('2026-11-06', TODAY)).toBe('d0_30');
    expect(ageingBucket('2026-09-08', TODAY)).toBe('d31_60');
    expect(ageingBucket('2026-08-01', TODAY)).toBe('d90plus');
    const a = ageing(
      [
        { due_on: '2026-11-01', amount_cents: 10_001, overdue: true, status: 'issued' },
        { due_on: '2026-10-30', amount_cents: 20_000, overdue: true, status: 'issued' },
        { due_on: '2026-08-01', amount_cents: 5, overdue: true, status: 'issued' },
        { due_on: '2026-08-01', amount_cents: 7, overdue: false, status: 'paid' },
      ],
      TODAY,
    );
    expect(a.d0_30).toEqual({ count: 2, cents: 30_001 });
    expect(a.d90plus).toEqual({ count: 1, cents: 5 });
  });

  it('enables bulk actions only for homogeneous selections', () => {
    expect(bulkActions([])).toEqual({ issue: false, remind: false, settle: false });
    expect(bulkActions([{ status: 'scheduled' }])).toEqual({
      issue: true,
      remind: false,
      settle: true,
    });
    expect(bulkActions([{ status: 'issued' }, { status: 'paid' }])).toEqual({
      issue: false,
      remind: false,
      settle: false,
    });
  });

  it('checks Σ = total on the live instalments only', () => {
    expect(planBalanced({ total_cents: 100, sum_cents: 100 })).toBe(true);
    expect(planBalanced({ total_cents: 100, sum_cents: 99 })).toBe(false);
    expect(
      liveSum([
        { amount_cents: 33, status: 'paid' },
        { amount_cents: 67, status: 'scheduled' },
        { amount_cents: 50, status: 'cancelled' },
      ]),
    ).toBe(100);
  });
});
