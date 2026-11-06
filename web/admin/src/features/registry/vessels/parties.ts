import type { Schemas, VesselParty } from '@porto/shared/api';

import { formatShare, parseSharePercent } from '../lib/format';

/** One row of the parties editor; the share is the text typed in percent. */
export type PartyRow = {
  key: string;
  customer_id: string;
  display_name: string;
  role: Schemas['VesselPartyRole'];
  share: string;
};

let counter = 0;
export const newRowKey = () => `p${++counter}`;

export function partiesToRows(parties: VesselParty[], locale = 'it'): PartyRow[] {
  return parties.map((p) => ({
    key: newRowKey(),
    customer_id: p.customer_id,
    display_name: p.display_name,
    role: p.role,
    share: p.role === 'owner' ? formatShare(p.share_millis, locale) : '',
  }));
}

export type PartiesCheck =
  | { ok: true; parties: Schemas['VesselPartyInput'][]; ownerTotal: number }
  | { ok: false; errors: Record<string, string>; ownerTotal: number };

/**
 * Mirrors the server rules (m1-plan §4.4): every row has a customer; owners need a share (0.1 %..100 %) and the
 * owner shares sum to at most 1000 ‰; the same customer and role appear once.
 */
export function checkParties(rows: PartyRow[]): PartiesCheck {
  const errors: Record<string, string> = {};
  const parties: Schemas['VesselPartyInput'][] = [];
  const seen = new Set<string>();
  let ownerTotal = 0;
  for (const r of rows) {
    if (!r.customer_id) {
      errors[r.key] = 'vessels:parties.validation.customerRequired';
      continue;
    }
    const dup = `${r.customer_id}|${r.role}`;
    if (seen.has(dup)) {
      errors[r.key] = 'vessels:parties.validation.duplicate';
      continue;
    }
    seen.add(dup);
    if (r.role === 'owner') {
      const millis = parseSharePercent(r.share);
      if (millis === null) {
        errors[r.key] = r.share.trim()
          ? 'vessels:parties.validation.shareInvalid'
          : 'vessels:parties.validation.shareRequired';
        continue;
      }
      ownerTotal += millis;
      parties.push({ customer_id: r.customer_id, role: r.role, share_millis: millis });
    } else {
      parties.push({ customer_id: r.customer_id, role: r.role });
    }
  }
  if (ownerTotal > 1000) errors.total = 'vessels:parties.validation.sharesExceed';
  return Object.keys(errors).length > 0
    ? { ok: false, errors, ownerTotal }
    : { ok: true, parties, ownerTotal };
}
