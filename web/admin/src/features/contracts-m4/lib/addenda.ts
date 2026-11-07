// Addendum form → API body by kind (spec 13.5, m4-plan §3.5). Pure.
import type { AddendumInput, AddendumKind } from '@porto/shared/api';

import type { CustomerChoice } from '../../desk/components/CustomerField';

export const ADDENDUM_KINDS: readonly AddendumKind[] = [
  'service_add',
  'service_remove',
  'parking',
  'billing_customer',
  'price_change',
  'vessel_change',
];

export type AddendumDraft = {
  kind: AddendumKind | '';
  effectiveOn: string | null;
  itemId: string;
  lineId: string;
  landResourceId: string;
  customer: CustomerChoice;
  vesselId: string;
  newTotalCents: number | null;
  reason: string;
};

export type AddendumMapping = { ok: true; body: AddendumInput } | { ok: false; error: string };

/** Body of an addendum by kind (§3.5): each kind needs its own payload field. */
export function addendumBody(d: AddendumDraft): AddendumMapping {
  if (!d.kind) return { ok: false, error: 'kind' };
  if (!d.effectiveOn) return { ok: false, error: 'effective' };
  const reason = d.reason.trim();
  const p: AddendumInput['payload'] = {};
  switch (d.kind) {
    case 'service_add':
      if (!d.itemId.trim()) return { ok: false, error: 'item' };
      p.item_id = d.itemId.trim();
      break;
    case 'service_remove':
      if (!d.lineId.trim()) return { ok: false, error: 'line' };
      p.line_id = d.lineId.trim();
      break;
    case 'parking':
      if (!d.landResourceId.trim()) return { ok: false, error: 'land' };
      p.land_resource_id = d.landResourceId.trim();
      break;
    case 'billing_customer':
      if (!d.customer) return { ok: false, error: 'customer' };
      p.customer_id = d.customer.id;
      break;
    case 'price_change':
      if (d.newTotalCents === null || d.newTotalCents < 0) return { ok: false, error: 'total' };
      if (!reason) return { ok: false, error: 'reason' };
      p.new_total_cents = d.newTotalCents;
      break;
    case 'vessel_change':
      if (!d.vesselId) return { ok: false, error: 'vessel' };
      p.vessel_id = d.vesselId;
      break;
  }
  if (reason) p.reason = reason;
  return { ok: true, body: { kind: d.kind, effective_on: d.effectiveOn, payload: p } };
}
