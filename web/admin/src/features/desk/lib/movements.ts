import type { Movements } from '@porto/shared/api';

export type MovementSection = keyof Omit<Movements, 'date'>;

/** Sections in order and the one-tap presence of each (arrivals → Arrivato, departures → Partito). */
export const MOVEMENT_SECTIONS: { key: MovementSection; action: 'arrived' | 'departed' | null }[] =
  [
    { key: 'arrivals_expected', action: 'arrived' },
    { key: 'departures_expected', action: 'departed' },
    { key: 'vessels_out', action: 'arrived' },
    { key: 'arrived_without_booking', action: null },
    { key: 'arrived', action: null },
    { key: 'departed', action: null },
  ];

export type LocateRef = { resource_type: string; resource_id: string; map_id?: string };
