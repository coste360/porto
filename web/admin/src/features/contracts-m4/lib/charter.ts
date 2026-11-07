// Charter wizard and rotation board helpers (spec 13.6, m4-plan §3.5, D7). Pure.
import type { PlacementInput, Schemas } from '@porto/shared/api';

export type CharterDraft = {
  fleetId: string;
  startsOn: string | null;
  endsOn: string | null;
  mode: Schemas['CharterPricingMode'];
  flatCents: number | null;
  flatReason: string;
  /** 1 = Monday … 7 = Sunday (ISO weekday), as a string for the select. */
  changeoverWeekday: string;
  placements: { target: PlacementInput | null; vesselId: string }[];
  activate: boolean;
};

export type CharterMapping =
  | { ok: true; body: Schemas['CharterCreate'] }
  | { ok: false; error: 'fleet' | 'period' | 'placements' | 'vessel' | 'duplicate' | 'flat' };

export function charterBody(d: CharterDraft): CharterMapping {
  if (!d.fleetId) return { ok: false, error: 'fleet' };
  if (!d.startsOn || !d.endsOn || d.endsOn <= d.startsOn) return { ok: false, error: 'period' };
  const placed = d.placements.filter((p) => p.target);
  if (placed.length === 0) return { ok: false, error: 'placements' };
  if (placed.some((p) => !p.vesselId)) return { ok: false, error: 'vessel' };
  const vessels = new Set(placed.map((p) => p.vesselId));
  if (vessels.size !== placed.length) return { ok: false, error: 'duplicate' };
  if (d.mode === 'flat' && (d.flatCents === null || d.flatCents < 0 || !d.flatReason.trim())) {
    return { ok: false, error: 'flat' };
  }
  return {
    ok: true,
    body: {
      fleet_id: d.fleetId,
      starts_on: d.startsOn,
      ends_on: d.endsOn,
      placements: placed.map((p) => ({ target: p.target!, vessel_id: p.vesselId })),
      pricing_mode: d.mode,
      changeover_weekday: Number(d.changeoverWeekday),
      ...(d.mode === 'flat' ? { flat_cents: d.flatCents!, flat_reason: d.flatReason.trim() } : {}),
      ...(d.activate ? { activate: true } : {}),
    },
  };
}

/** line id → vessel id on the board. */
export type Rotation = Record<string, string>;

/** Assignments of the lines that change; a vessel may sit on one line only (a swap lists both lines). */
export function rotationAssignments(
  current: Rotation,
  next: Rotation,
): Schemas['CharterAssignment'][] {
  return Object.entries(next)
    .filter(([line, vessel]) => vessel && current[line] !== vessel)
    .map(([line_id, vessel_id]) => ({ line_id, vessel_id }));
}

/** Moves a vessel onto a line; if it was on another line, the two lines swap their vessels. */
export function dropVessel(board: Rotation, lineId: string, vesselId: string): Rotation {
  const from = Object.keys(board).find((l) => board[l] === vesselId);
  const next = { ...board, [lineId]: vesselId };
  if (from && from !== lineId) next[from] = board[lineId] ?? '';
  return next;
}
