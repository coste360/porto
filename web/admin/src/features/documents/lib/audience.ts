// Audience builder of a mass communication (spec 28, m4-plan §6.7): one of three modes — a filter over contracts
// and presences, an explicit customer list, or a selection of berths on the map. The builder state is pure; the
// server resolves the audience and counts the skipped recipients (consent, email) in the preview.
import type { Schemas } from '@porto/shared/api';

type Audience = Schemas['CommAudience'];

export type AudienceMode = 'filter' | 'customers' | 'map';

export type AudienceDraft = {
  mode: AudienceMode;
  groups: string[];
  structures: string[];
  contractKinds: Schemas['ContractKind'][];
  terms: Schemas['ContractTerm'][];
  holders: boolean;
  presentFrom: string | null;
  presentTo: string | null;
  deadlinesWithinDays: string;
  customerIds: string[];
  mapId: string;
  resourceIds: string[];
};

export const EMPTY_AUDIENCE: AudienceDraft = {
  mode: 'filter',
  groups: [],
  structures: [],
  contractKinds: [],
  terms: [],
  holders: false,
  presentFrom: null,
  presentTo: null,
  deadlinesWithinDays: '',
  customerIds: [],
  mapId: '',
  resourceIds: [],
};

/** Toggles a value in a list (chips of the filter). */
export function toggle<T>(list: readonly T[], value: T): T[] {
  return list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
}

export type AudienceResult =
  | { ok: true; audience: Audience }
  | { ok: false; error: 'empty' | 'range' | 'days' | 'customers' | 'selection' };

/** Builds the API audience of the current mode only (the other modes' values are not sent). */
export function buildAudience(d: AudienceDraft): AudienceResult {
  switch (d.mode) {
    case 'customers':
      return d.customerIds.length === 0
        ? { ok: false, error: 'customers' }
        : { ok: true, audience: { customer_ids: [...new Set(d.customerIds)] } };
    case 'map':
      return !d.mapId || d.resourceIds.length === 0
        ? { ok: false, error: 'selection' }
        : { ok: true, audience: { map_id: d.mapId, resource_ids: [...new Set(d.resourceIds)] } };
    case 'filter': {
      const a: Audience = {};
      if (d.groups.length) a.groups = d.groups;
      if (d.structures.length) a.structures = d.structures;
      if (d.contractKinds.length) a.contract_kinds = d.contractKinds;
      if (d.terms.length) a.terms = d.terms;
      if (d.holders) a.holders = true;
      if (d.presentFrom) a.present_from = d.presentFrom;
      if (d.presentTo) a.present_to = d.presentTo;
      if (d.presentFrom && d.presentTo && d.presentFrom > d.presentTo) {
        return { ok: false, error: 'range' };
      }
      const days = d.deadlinesWithinDays.trim();
      if (days) {
        if (!/^\d{1,3}$/.test(days) || Number(days) < 1 || Number(days) > 365) {
          return { ok: false, error: 'days' };
        }
        a.with_deadlines_within_days = Number(days);
      }
      return Object.keys(a).length === 0
        ? { ok: false, error: 'empty' }
        : { ok: true, audience: a };
    }
  }
}

/** Draft from a saved audience (the mode is the one whose fields are set). */
export function draftFromAudience(a: Audience | undefined): AudienceDraft {
  if (!a) return EMPTY_AUDIENCE;
  if (a.customer_ids?.length)
    return { ...EMPTY_AUDIENCE, mode: 'customers', customerIds: a.customer_ids };
  if (a.map_id) {
    return { ...EMPTY_AUDIENCE, mode: 'map', mapId: a.map_id, resourceIds: a.resource_ids ?? [] };
  }
  return {
    ...EMPTY_AUDIENCE,
    groups: a.groups ?? [],
    structures: a.structures ?? [],
    contractKinds: a.contract_kinds ?? [],
    terms: a.terms ?? [],
    holders: a.holders ?? false,
    presentFrom: a.present_from ?? null,
    presentTo: a.present_to ?? null,
    deadlinesWithinDays: a.with_deadlines_within_days ? String(a.with_deadlines_within_days) : '',
  };
}

/** Number of recipients that will receive the message, from the preview counts. */
export function deliverable(p: Schemas['CommunicationPreview']['recipients']): number {
  return Math.max(0, p.count - p.skipped_no_consent - p.skipped_no_email);
}

/** Steps of the communication wizard (m4-plan §8.1). */
export const COMM_STEPS = ['audience', 'content', 'attachments', 'preview', 'send'] as const;
export type CommStep = (typeof COMM_STEPS)[number];
