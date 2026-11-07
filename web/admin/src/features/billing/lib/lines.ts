// Live preview of charge lines (new charge, counter sale, adjustment dialog; m5-plan §9.1). Integer cents with
// BigInt half-even divisions, mirroring Go money.DivRoundHalfEven; the server recomputes the lines through
// chargeplan and is authoritative: this is the preview shown while typing.

/** num / den rounded half-even (den > 0). */
export function divHalfEven(num: bigint, den: bigint): bigint {
  const neg = num < 0n;
  const a = neg ? -num : num;
  const q = a / den;
  const r = a % den;
  const twice = r * 2n;
  let out = q;
  if (twice > den || (twice === den && q % 2n === 1n)) out = q + 1n;
  return neg ? -out : out;
}

export type PreviewLineInput = {
  unitCents: number;
  qtyMilli: number;
  discountCents?: number;
  vatRateBp: number;
  priceIncludesVat: boolean;
};

export type PreviewLine = { net: number; vat: number; gross: number };

/** Net, VAT and gross of one line. */
export function previewLine(l: PreviewLineInput): PreviewLine {
  const amount =
    divHalfEven(BigInt(l.unitCents) * BigInt(l.qtyMilli), 1000n) - BigInt(l.discountCents ?? 0);
  const rate = BigInt(l.vatRateBp);
  if (l.priceIncludesVat) {
    const vat = divHalfEven(amount * rate, 10_000n + rate);
    return { net: Number(amount - vat), vat: Number(vat), gross: Number(amount) };
  }
  const vat = divHalfEven(amount * rate, 10_000n);
  return { net: Number(amount), vat: Number(vat), gross: Number(amount + vat) };
}

export type VatRow = { vatRateBp: number; net: number; vat: number; gross: number };

/** Totals per VAT rate (ascending rate) and grand totals. */
export function previewTotals(lines: readonly (PreviewLineInput | null)[]): {
  byRate: VatRow[];
  net: number;
  vat: number;
  gross: number;
} {
  const by = new Map<number, VatRow>();
  let net = 0;
  let vat = 0;
  let gross = 0;
  for (const l of lines) {
    if (!l) continue;
    const p = previewLine(l);
    const row = by.get(l.vatRateBp) ?? { vatRateBp: l.vatRateBp, net: 0, vat: 0, gross: 0 };
    row.net += p.net;
    row.vat += p.vat;
    row.gross += p.gross;
    by.set(l.vatRateBp, row);
    net += p.net;
    vat += p.vat;
    gross += p.gross;
  }
  return { byRate: [...by.values()].sort((a, b) => a.vatRateBp - b.vatRateBp), net, vat, gross };
}

/** Quantity typed by staff ("2", "1,5") → milli; null when invalid or not positive. */
export function parseQty(text: string): number | null {
  const m = /^\s*(\d+)(?:[.,](\d{1,3}))?\s*$/.exec(text);
  if (!m) return null;
  const milli = Number(m[1]) * 1000 + Number((m[2] ?? '').padEnd(3, '0') || '0');
  return milli > 0 && Number.isSafeInteger(milli) ? milli : null;
}
