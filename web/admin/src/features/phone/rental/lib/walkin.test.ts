import { describe, expect, it } from 'vitest';

import { slotNowIn, walkInHours, walkInRange } from './walkin';

describe('walk-in', () => {
  it('reads the slot in progress in the marina timezone', () => {
    // 08:40 UTC = 10:40 in Rome (summer time): slot 21 (10:30-11:00).
    expect(slotNowIn('Europe/Rome', new Date('2027-06-12T08:40:00Z'))).toEqual({
      date: '2027-06-12',
      slot: 21,
    });
    // 22:10 UTC on the 11th is already the 12th in Rome.
    expect(slotNowIn('Europe/Rome', new Date('2027-06-11T22:10:00Z'))).toEqual({
      date: '2027-06-12',
      slot: 0,
    });
  });

  it('offers whole hours within the open window only', () => {
    expect(walkInHours({ date: '2027-06-12', slot: 33 }, 16, 38)).toEqual([1, 2]);
    expect(walkInHours({ date: '2027-06-12', slot: 38 }, 16, 38)).toEqual([]);
    expect(walkInHours({ date: '2027-06-12', slot: 10 }, 16, 38)).toEqual([]);
  });

  it('builds the range of a duration', () => {
    expect(walkInRange({ date: '2027-06-12', slot: 21 }, 3)).toEqual({
      start: { date: '2027-06-12', slot: 21 },
      end: { date: '2027-06-12', slot: 27 },
    });
  });
});
