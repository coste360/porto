// Fixtures of the booking flow tests (shapes of the generated API types).
import type {
  Booking,
  BookingAvailability,
  BookingConfig,
  BookingCreated,
  BookingQuote,
} from '@porto/shared/api';

export const SLUG = 'prova';

export const config: BookingConfig = {
  enabled: true,
  payable: true,
  currency: 'EUR',
  rules: {
    min_nights: 1,
    max_nights: 30,
    min_advance_hours: 2,
    max_advance_days: 365,
    arrival_from: '12:00',
    arrival_to: '20:00',
    closed_weekdays: [],
  },
  categories: [{ code: 'B', name: { it: 'Posto 10-12 m', en: 'Berth 10-12 m' } }],
  services: [
    {
      item_id: 'svc-power',
      name: { it: 'Corrente', en: 'Power' },
      unit: 'night',
      unit_price_cents: 500,
      default_selected: true,
    },
  ],
  payment_mode: 'full',
  deposit_bp: 3000,
  cancellation_tiers: [
    { min_days_before: 7, refund_bp: 10000 },
    { min_days_before: 0, refund_bp: 0 },
  ],
  approval_always: false,
  required_fields: {
    documents: ['insurance_policy'],
    people_on_board: true,
    phone: true,
    documents_later: false,
  },
  texts: [
    { kind: 'terms', version: 2, text: { it: 'Condizioni', en: 'Terms' } },
    { kind: 'privacy', version: 1, text: { it: 'Privacy', en: 'Privacy' } },
  ],
  theme: { preset: 'default', hide_header_in_embed: false },
  arrival_info: { vhf_channel: '9', phone: '+39 010 000000' },
  show_feet: true,
};

export const availability: BookingAvailability = {
  arrival: '2030-07-01',
  departure: '2030-07-04',
  nights: 3,
  categories: [
    {
      code: 'B',
      name: { it: 'Posto 10-12 m' },
      available: true,
      from_cents: 15000,
      approval_required: false,
    },
    { code: 'C', name: { it: 'Posto 14 m' }, available: false, approval_required: false },
  ],
};

export const emptyAvailability: BookingAvailability = {
  arrival: '2030-07-01',
  departure: '2030-07-04',
  nights: 3,
  categories: [
    { code: 'B', name: { it: 'Posto 10-12 m' }, available: false, approval_required: false },
  ],
};

export const quote: BookingQuote = {
  category: 'B',
  nights: 3,
  lines: [
    { kind: 'berth', label: { it: 'Ormeggio 3 notti' }, qty_milli: 3000, total_cents: 15000 },
    {
      kind: 'service',
      label: { it: 'Corrente' },
      item_id: 'svc-power',
      qty_milli: 1000,
      total_cents: 1500,
    },
  ],
  total_cents: 16500,
  net_cents: 13525,
  vat_cents: 2975,
  due_now_cents: 16500,
  payment_mode: 'full',
  cancellation_tiers: config.cancellation_tiers,
  promo_applied: false,
  approval_required: false,
  currency: 'EUR',
};

export function created(holdExpiresAt: string): BookingCreated {
  return {
    booking_id: 'bk-0001-aaaa',
    reference: 'K7M2Q9XA',
    booking_token: 'tok-secret',
    status: 'held',
    hold_expires_at: holdExpiresAt,
    quote,
    approval_required: false,
    amount_due_now_cents: 16500,
  };
}

export function booking(holdExpiresAt: string, patch: Partial<Booking> = {}): Booking {
  return {
    booking_id: 'bk-0001-aaaa',
    reference: 'K7M2Q9XA',
    status: 'held',
    arrival: '2030-07-01',
    departure: '2030-07-04',
    category: 'B',
    vessel: { name: '—', loa_cm: 1150, beam_cm: 380 },
    hold_expires_at: holdExpiresAt,
    quote,
    approval_required: false,
    amount_due_now_cents: 16500,
    details_complete: false,
    documents: [],
    missing_documents: ['insurance_policy'],
    ...patch,
  };
}
