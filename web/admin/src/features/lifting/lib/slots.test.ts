import type { EquipmentBooking } from '@porto/shared/api';

import { lanes, overlaps, slotLabel, slotOf, slotRanges, slotsFor, windowSlots } from './slots';

const booking = (
  id: string,
  eq: string,
  from: number,
  to: number,
  status: EquipmentBooking['status'] = 'booked',
): EquipmentBooking => ({
  id,
  equipment_id: eq,
  from_slot: from,
  to_slot: to,
  purpose: 'haul_out',
  status,
  source: 'office',
  note: '',
  version: 1,
  created_at: '2027-06-01T00:00:00Z',
  updated_at: '2027-06-01T00:00:00Z',
});

describe('lifting slots', () => {
  it('converts between slots and wall-clock times', () => {
    expect(slotLabel(28)).toBe('07:00');
    expect(slotLabel(29)).toBe('07:15');
    expect(slotLabel(96)).toBe('24:00');
    expect(slotOf('07:14')).toBe(28);
    expect(slotOf('19:00')).toBe(76);
    expect(slotOf('25:00')).toBeNull();
    expect(windowSlots(28, 32)).toEqual([28, 29, 30, 31]);
    expect(slotsFor(30)).toBe(2);
    expect(slotsFor(31)).toBe(3);
    expect(slotsFor(0)).toBe(1);
  });

  it('lays bookings out per machine, ignoring requests and cancelled ones', () => {
    const l = lanes([
      booking('a', 'tl', 32, 36),
      booking('b', 'tl', 40, 42, 'done'),
      booking('c', 'tl', 44, 46, 'cancelled'),
      booking('d', 'gr', 32, 33),
    ]);
    expect(l.get('tl')?.bySlot.get(35)?.id).toBe('a');
    expect(l.get('tl')?.bySlot.has(44)).toBe(false);
    expect(overlaps(l.get('tl'), 36, 40)).toBe(false);
    expect(overlaps(l.get('tl'), 35, 37)).toBe(true);
    expect(overlaps(l.get('tl'), 35, 37, 'a')).toBe(false);
    expect(overlaps(undefined, 0, 4)).toBe(false);
  });

  it('describes the slots of a conflict as ranges', () => {
    expect(slotRanges([35, 34, 40])).toEqual(['08:30-09:00', '10:00-10:15']);
    expect(slotRanges([])).toEqual([]);
  });
});
