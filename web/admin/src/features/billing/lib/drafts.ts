// Draft lines of the charge, counter sale and adjustment editors (m5-plan §9.1) and their conversion to the API
// line inputs. Quantities are typed as text and parsed to milli; prices stay integer cents.
import type { Charge, components } from '@porto/shared/api';

import { parseQty, type PreviewLineInput } from './lines';
import { oldestFirst, type MatchRow } from './matching';

type Item = components['schemas']['Item'];

export type LineDraft = {
  key: number;
  itemId: string | null;
  qty: string;
  unitCents: number | null;
  discountCents: number | null;
};

let nextKey = 1;
export function emptyLine(): LineDraft {
  return { key: nextKey++, itemId: null, qty: '1', unitCents: null, discountCents: null };
}

export function itemName(item: Pick<Item, 'name' | 'code'>, lang: string): string {
  const n = item.name as { it?: string; en?: string };
  return (lang.startsWith('en') ? n.en || n.it : n.it || n.en) || item.code;
}

/** Preview input of a draft line (null while item, quantity or price are missing). */
export function previewInput(
  l: LineDraft,
  items: ReadonlyMap<string, Item>,
  allowNegative = false,
) {
  const item = l.itemId ? items.get(l.itemId) : undefined;
  const qty = allowNegative ? parseSignedQty(l.qty) : parseQty(l.qty);
  if (!item || qty === null || l.unitCents === null) return null;
  const p: PreviewLineInput = {
    unitCents: l.unitCents,
    qtyMilli: qty,
    vatRateBp: item.vat_rate_bp,
    priceIncludesVat: item.price_includes_vat,
    ...(l.discountCents ? { discountCents: l.discountCents } : {}),
  };
  return p;
}

/** Signed quantity for adjustment lines ("-1", "2,5"). */
export function parseSignedQty(text: string): number | null {
  const t = text.trim();
  const neg = t.startsWith('-');
  const q = parseQty(neg ? t.slice(1) : t);
  return q === null ? null : neg ? -q : q;
}

/** Every line has an item and a valid quantity (unit price optional: default from the price list). */
export function linesValid(lines: readonly LineDraft[], allowNegative = false): boolean {
  return (
    lines.length > 0 &&
    lines.every(
      (l) => !!l.itemId && (allowNegative ? parseSignedQty(l.qty) : parseQty(l.qty)) !== null,
    )
  );
}

/** Body lines of create/patch/counter sale from the drafts. */
export function toLineInputs(lines: readonly LineDraft[], allowNegative = false) {
  return lines.map((l) => ({
    item_id: l.itemId ?? '',
    qty_milli: (allowNegative ? parseSignedQty(l.qty) : parseQty(l.qty)) ?? 0,
    ...(l.unitCents !== null ? { unit_cents: l.unitCents } : {}),
    ...(l.discountCents ? { discount_cents: l.discountCents } : {}),
  }));
}

/** Drafts from the stored lines (for edit in place and replace). */
export function draftsOf(charge: Charge): LineDraft[] {
  return (charge.lines ?? [])
    .filter((l) => !!l.item_id)
    .map((l) => ({
      ...emptyLine(),
      itemId: l.item_id ?? null,
      qty: String(l.qty_milli / 1000).replace('.', ','),
      unitCents: l.unit_cents,
      discountCents: l.discount_cents || null,
    }));
}

export function toMatchRows(charges: readonly Charge[]): MatchRow[] {
  return oldestFirst(
    charges
      .filter((c) => c.payment_status !== 'cancelled' && c.residual_cents > 0)
      .map((c) => ({
        chargeId: c.id,
        number: c.number,
        dueOn: c.due_on,
        residualCents: c.residual_cents,
      })),
  );
}
