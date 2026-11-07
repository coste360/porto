// Public surface of the operational map for WEB-DESK (m3-plan §8.7; signatures fixed). WEB-DESK imports map
// pieces only from here and from @porto/shared/map.
export { PlacementPicker, type PlacementPickerProps } from './PlacementPicker';

/** Search params of /m/$marinaId/ops that open the operational view on a map, date, element or lens. */
export function opsMapSearch(p: {
  mapId?: string;
  date?: string;
  focus?: string;
  lens?: string;
}): Record<string, string> {
  const out: Record<string, string> = {};
  if (p.mapId) out.map = p.mapId;
  if (p.date) out.date = p.date;
  if (p.focus) out.focus = p.focus;
  if (p.lens) out.lens = p.lens;
  return out;
}
