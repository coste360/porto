import type { Schemas } from '@porto/shared/api';

type HullType = Schemas['HullType'];

/** Dimensions of the chosen vessel, integer centimetres (for search, fit preview and the map picker). */
export type VesselDims = {
  loa_cm: number;
  beam_cm: number;
  hull_type: HullType;
  draft_cm?: number;
};

export type NewVesselDraft = {
  name: string;
  loaCm: number | null;
  beamCm: number | null;
  hullType: HullType;
  draftCm: number | null;
};

export type VesselValue =
  | { kind: 'existing'; id: string | null; label: string; dims: VesselDims | null }
  | ({ kind: 'new' } & NewVesselDraft);

export const NO_VESSEL: VesselValue = { kind: 'existing', id: null, label: '', dims: null };

export function emptyNewVessel(): VesselValue {
  return {
    kind: 'new',
    name: '',
    loaCm: null,
    beamCm: null,
    hullType: 'monohull_motor',
    draftCm: null,
  };
}

/** The new-vessel body of createTransit / unexpected arrival, or null when incomplete. */
export function newVesselInput(v: VesselValue): Schemas['NewVesselInput'] | null {
  if (v.kind !== 'new') return null;
  if (!v.name.trim() || !v.loaCm || !v.beamCm || v.loaCm <= 0 || v.beamCm <= 0) return null;
  return {
    name: v.name.trim(),
    loa_cm: v.loaCm,
    beam_cm: v.beamCm,
    hull_type: v.hullType,
    ...(v.draftCm && v.draftCm > 0 ? { draft_cm: v.draftCm } : {}),
  };
}

export function vesselDims(v: VesselValue): VesselDims | null {
  if (v.kind === 'existing') return v.dims;
  if (!v.loaCm || !v.beamCm) return null;
  return { loa_cm: v.loaCm, beam_cm: v.beamCm, hull_type: v.hullType };
}

export function vesselReady(v: VesselValue): boolean {
  return v.kind === 'existing' ? v.id !== null : newVesselInput(v) !== null;
}
