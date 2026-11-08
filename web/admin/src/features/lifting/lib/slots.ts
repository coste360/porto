// Pure helpers of the lifting day calendar (m10-plan §11, §14.3): 15-minute slots of the wall-clock day
// (0..95, to_slot exclusive), labels, the columns of the lift window and the bookings laid out per machine.
import type { EquipmentBooking } from '@porto/shared/api';

export const SLOTS_PER_DAY = 96;

/** "HH:MM" of the start of a slot (96 = "24:00"). */
export function slotLabel(slot: number): string {
  const h = Math.floor(slot / 4);
  const m = (slot % 4) * 15;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
}

/** Slot of an "HH:MM" time (minutes rounded down to the slot), null when invalid. */
export function slotOf(time: string): number | null {
  const m = /^(\d{1,2}):(\d{2})$/.exec(time.trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  return h * 4 + Math.floor(min / 15);
}

/** The slots of the window [from, to). */
export function windowSlots(from: number, to: number): number[] {
  const out: number[] = [];
  for (let s = Math.max(from, 0); s < Math.min(to, SLOTS_PER_DAY); s++) out.push(s);
  return out;
}

/** Number of slots covering minutes (at least one). */
export function slotsFor(minutes: number): number {
  return Math.max(1, Math.ceil(minutes / 15));
}

export type Lane = {
  equipmentId: string;
  /** slot -> booking occupying it */
  bySlot: Map<number, EquipmentBooking>;
};

/** Booked and done bookings of the day per machine, indexed by slot. */
export function lanes(bookings: readonly EquipmentBooking[]): Map<string, Lane> {
  const out = new Map<string, Lane>();
  for (const b of bookings) {
    if (!b.equipment_id || b.from_slot === undefined || b.to_slot === undefined) continue;
    if (b.status !== 'booked' && b.status !== 'done') continue;
    let lane = out.get(b.equipment_id);
    if (!lane) {
      lane = { equipmentId: b.equipment_id, bySlot: new Map() };
      out.set(b.equipment_id, lane);
    }
    for (let s = b.from_slot; s < b.to_slot; s++) lane.bySlot.set(s, b);
  }
  return out;
}

/** True when [from, to) on a machine overlaps a booking other than `except` (UI hint only: the server decides). */
export function overlaps(
  lane: Lane | undefined,
  from: number,
  to: number,
  except?: string,
): boolean {
  if (!lane) return false;
  for (let s = from; s < to; s++) {
    const b = lane.bySlot.get(s);
    if (b && b.id !== except) return true;
  }
  return false;
}

/** Slots of a 409 equipment_slot_taken / operator_busy problem, as "HH:MM-HH:MM" ranges. */
export function slotRanges(slots: readonly number[]): string[] {
  const sorted = [...slots].sort((a, b) => a - b);
  const out: string[] = [];
  let start: number | null = null;
  let prev = -2;
  for (const s of sorted) {
    if (start === null) start = s;
    else if (s !== prev + 1) {
      out.push(`${slotLabel(start)}-${slotLabel(prev + 1)}`);
      start = s;
    }
    prev = s;
  }
  if (start !== null) out.push(`${slotLabel(start)}-${slotLabel(prev + 1)}`);
  return out;
}
