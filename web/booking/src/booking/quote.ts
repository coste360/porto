import type { BookingQuoteInput, BookingServiceOption } from '@porto/shared/api';

import type { SearchInput } from './state';

/** Selected services, falling back to the defaults until the customer changes something. */
export function effectiveServices(
  options: BookingServiceOption[],
  chosen: Record<string, number>,
  touched: boolean,
): Record<string, number> {
  if (touched) return chosen;
  return Object.fromEntries(
    options.filter((o) => o.default_selected).map((o) => [o.item_id, 1000]),
  );
}

export function quoteInput(
  search: SearchInput,
  category: string,
  services: Record<string, number>,
  promo: string,
): BookingQuoteInput {
  return {
    arrival: search.arrival,
    departure: search.departure,
    category,
    vessel: {
      loa_cm: search.loa_cm,
      beam_cm: search.beam_cm,
      ...(search.draft_cm ? { draft_cm: search.draft_cm } : {}),
      ...(search.hull_type ? { hull_type: search.hull_type } : {}),
    },
    services: Object.entries(services)
      .filter(([, q]) => q > 0)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([item_id, qty_milli]) => ({ item_id, qty_milli })),
    ...(promo ? { promo_code: promo } : {}),
  };
}
