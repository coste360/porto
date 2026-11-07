// State of the renewal campaign wizard (spec 13.8, m4-plan §3.4, §8.1): 1 filter → 2 preview → 3 message and
// deadline → 4 launch. The reducer is pure; the page runs the API calls (create, preview, patch, launch) and feeds
// their results back as actions. Preview holds the berths (D4), so going back to the filter after a preview means
// a new preview (the server recomputes idempotently).
import type { Schemas } from '@porto/shared/api';

type Filter = Schemas['RenewalFilter'];
type Campaign = Schemas['RenewalCampaign'];
type Preview = Schemas['RenewalCampaignPreview'];
type Proposal = Schemas['RenewalProposal'];

export const WIZARD_STEPS = ['filter', 'preview', 'message', 'launch'] as const;
export type WizardStep = (typeof WIZARD_STEPS)[number];

export type WizardState = {
  step: WizardStep;
  name: string;
  filter: Filter;
  priceListId: string;
  deadlineOn: string;
  message: string;
  campaign: Campaign | null;
  preview: Preview | null;
  /** The filter changed after the last preview: the preview must be recomputed before going on. */
  stale: boolean;
};

export type WizardAction =
  | { type: 'name'; value: string }
  | { type: 'filter'; patch: Partial<Filter> }
  | { type: 'priceList'; value: string }
  | { type: 'deadline'; value: string }
  | { type: 'message'; value: string }
  | { type: 'saved'; campaign: Campaign }
  | { type: 'previewed'; preview: Preview }
  | { type: 'proposalChanged'; proposal: Proposal }
  | { type: 'goto'; step: WizardStep }
  | { type: 'next' }
  | { type: 'back' };

export function initialWizard(deadlineOn: string, campaign?: Campaign | null): WizardState {
  return {
    step: 'filter',
    name: campaign?.name ?? '',
    filter: campaign?.filter ?? {},
    priceListId: campaign?.price_list_id ?? '',
    deadlineOn: campaign?.deadline_on ?? deadlineOn,
    message: campaign?.message ?? '',
    campaign: campaign ?? null,
    preview: null,
    stale: false,
  };
}

/** Drops empty arrays and empty strings: the server reads a missing key as "no constraint". */
export function cleanFilter(f: Filter): Filter {
  const out: Filter = {};
  for (const [k, v] of Object.entries(f) as [keyof Filter, unknown][]) {
    if (v === undefined || v === null || v === '') continue;
    if (Array.isArray(v) && v.length === 0) continue;
    (out as Record<string, unknown>)[k] = v;
  }
  return out;
}

export function filterIsEmpty(f: Filter): boolean {
  return Object.keys(cleanFilter(f)).length === 0;
}

/** Why the wizard cannot leave a step, or null. Keys are `renewals:wizard.errors.<key>`. */
export function stepError(s: WizardState, today: string): string | null {
  switch (s.step) {
    case 'filter':
      if (!s.name.trim()) return 'name';
      if (filterIsEmpty(s.filter)) return 'filter';
      if (s.filter.ends_from && s.filter.ends_to && s.filter.ends_from > s.filter.ends_to) {
        return 'range';
      }
      return null;
    case 'preview':
      if (!s.preview || s.stale) return 'preview';
      if (includedCount(s.preview.proposals.items) === 0) return 'empty';
      return null;
    case 'message':
      if (!s.deadlineOn || s.deadlineOn <= today) return 'deadline';
      return null;
    case 'launch':
      return s.campaign?.status === 'draft' ? null : 'launched';
  }
}

export function includedCount(items: readonly Pick<Proposal, 'status'>[]): number {
  return items.filter((p) => p.status !== 'excluded').length;
}

/** Totals of the preview table, from the proposals themselves (integer cents). */
export function previewTotals(items: readonly Proposal[]) {
  let oldCents = 0;
  let newCents = 0;
  let included = 0;
  let excluded = 0;
  for (const p of items) {
    if (p.status === 'excluded') {
      excluded++;
      continue;
    }
    included++;
    oldCents += p.old_total_cents;
    newCents += p.new_total_cents ?? 0;
  }
  return { oldCents, newCents, deltaCents: newCents - oldCents, included, excluded };
}

/** Excluded proposals staff may include with a reason (never berth_unavailable, D5). */
export function canInclude(p: Pick<Proposal, 'status' | 'exclusion_reason'>): boolean {
  return p.status === 'excluded' && p.exclusion_reason !== 'berth_unavailable';
}

export function wizardReducer(s: WizardState, a: WizardAction): WizardState {
  switch (a.type) {
    case 'name':
      return { ...s, name: a.value };
    case 'filter':
      return { ...s, filter: { ...s.filter, ...a.patch }, stale: s.preview !== null };
    case 'priceList':
      return { ...s, priceListId: a.value, stale: s.preview !== null };
    case 'deadline':
      return { ...s, deadlineOn: a.value };
    case 'message':
      return { ...s, message: a.value };
    case 'saved':
      return { ...s, campaign: a.campaign };
    case 'previewed':
      return {
        ...s,
        preview: a.preview,
        campaign: a.preview.campaign,
        stale: false,
        step: 'preview',
      };
    case 'proposalChanged': {
      if (!s.preview) return s;
      const items = s.preview.proposals.items.map((p) => (p.id === a.proposal.id ? a.proposal : p));
      return { ...s, preview: { ...s.preview, proposals: { ...s.preview.proposals, items } } };
    }
    case 'goto': {
      const target = WIZARD_STEPS.indexOf(a.step);
      const current = WIZARD_STEPS.indexOf(s.step);
      // Forward jumps go through next (validation); backward jumps are always allowed.
      return target <= current ? { ...s, step: a.step } : s;
    }
    case 'back': {
      const i = WIZARD_STEPS.indexOf(s.step);
      return i > 0 ? { ...s, step: WIZARD_STEPS[i - 1]! } : s;
    }
    case 'next': {
      const i = WIZARD_STEPS.indexOf(s.step);
      return i < WIZARD_STEPS.length - 1 ? { ...s, step: WIZARD_STEPS[i + 1]! } : s;
    }
  }
}

/** Create body of the campaign from the wizard (step 1 → the first preview). */
export function campaignInput(s: WizardState): Schemas['RenewalCampaignInput'] {
  return {
    name: s.name.trim(),
    filter: cleanFilter(s.filter),
    deadline_on: s.deadlineOn,
    ...(s.priceListId ? { price_list_id: s.priceListId } : {}),
    ...(s.message.trim() ? { message: s.message.trim() } : {}),
  };
}

/** Patch of a saved draft campaign with what changed in the wizard. */
export function campaignPatch(s: WizardState): Schemas['RenewalCampaignPatch'] {
  return {
    name: s.name.trim(),
    filter: cleanFilter(s.filter),
    deadline_on: s.deadlineOn,
    price_list_id: s.priceListId || null,
    message: s.message.trim(),
  };
}

// ---- monitor ----

/** Proposal states in the order of the monitor counters. */
export const PROPOSAL_STATES: readonly Schemas['RenewalProposalStatus'][] = [
  'previewed',
  'proposed',
  'sent',
  'accepted',
  'signed',
  'completed',
  'declined',
  'expired',
  'excluded',
];

/** Counts by state from the campaign counters (server object of state → integer). */
export function campaignCounts(counts: Record<string, unknown> | undefined) {
  const out: Partial<Record<Schemas['RenewalProposalStatus'], number>> = {};
  for (const s of PROPOSAL_STATES) {
    const v = counts?.[s];
    if (typeof v === 'number' && v > 0) out[s] = v;
  }
  return out;
}

/** Actions the office may take on a proposal, by state (m4-plan §3.4 proposal table). */
export function proposalActions(p: Pick<Proposal, 'status' | 'exclusion_reason'>) {
  return {
    resend: p.status === 'proposed' || p.status === 'sent',
    accept: p.status === 'sent' || p.status === 'proposed',
    decline: p.status === 'proposed' || p.status === 'sent',
    include: canInclude(p),
  };
}

export function proposalVariant(
  status: Schemas['RenewalProposalStatus'],
): 'default' | 'secondary' | 'outline' | 'destructive' {
  switch (status) {
    case 'completed':
    case 'signed':
    case 'accepted':
      return 'default';
    case 'sent':
    case 'proposed':
      return 'secondary';
    case 'declined':
    case 'expired':
    case 'excluded':
      return 'destructive';
    default:
      return 'outline';
  }
}
