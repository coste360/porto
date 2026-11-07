// Mandate form → API body (spec 14.3, m4-plan §4.3). Pure.
import type { Schemas } from '@porto/shared/api';

export type MandateForm = {
  rightId: string;
  from: string | null;
  to: string | null;
  shareBp: number | null;
  minCents: number | null;
  channels: Schemas['MandateChannel'][];
  settlement: 'credit' | 'transfer' | '';
};

/** Mandate body (14.3): nights from today on, share 0..100 %, at least one channel. */
export function mandateBody(
  f: MandateForm,
  today: string,
): { ok: true; body: Schemas['MandateInput'] } | { ok: false; error: string } {
  if (!f.rightId) return { ok: false, error: 'right' };
  if (!f.from || !f.to || f.to < f.from) return { ok: false, error: 'period' };
  if (f.from < today) return { ok: false, error: 'past' };
  if (f.shareBp === null || f.shareBp < 0 || f.shareBp > 10_000)
    return { ok: false, error: 'share' };
  if (f.channels.length === 0) return { ok: false, error: 'channels' };
  if (!f.settlement) return { ok: false, error: 'settlement' };
  return {
    ok: true,
    body: {
      right_id: f.rightId,
      from_on: f.from,
      to_on: f.to,
      owner_share_bp: f.shareBp,
      ...(f.minCents ? { min_guaranteed_cents: f.minCents } : {}),
      channels: f.channels,
      settlement: f.settlement,
    },
  };
}
