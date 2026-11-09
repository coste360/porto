import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { queryKeys } from '../api/queryKeys';
import {
  OFFLINE_OPERATIONS,
  OFFLINE_OPERATION_KINDS,
  OFFLINE_QUERY_ALLOWLIST,
  isPersistable,
  localDate,
} from './allowlist';

const M = 'm-1';

/** The operations flagged `x-porto-offline: true` in api/openapi.yaml (operation-level keys, 6 spaces). */
function flaggedInContract(): string[] {
  const spec = readFileSync(resolve(__dirname, '../../../../api/openapi.yaml'), 'utf8');
  const out: string[] = [];
  let current: string | null = null;
  for (const line of spec.split('\n')) {
    if (/^ {4}(get|put|post|delete|patch):/.test(line)) current = null;
    const op = /^ {6}operationId: (\w+)/.exec(line);
    if (op) current = op[1]!;
    if (/^ {6}x-porto-offline: true/.test(line) && current) out.push(current);
  }
  return out.sort();
}

describe('offline allowlists', () => {
  it('OFFLINE_OPERATIONS equals the x-porto-offline operations of the contract', () => {
    expect([...OFFLINE_OPERATIONS].sort()).toEqual(flaggedInContract());
    expect(flaggedInContract()).toEqual([
      'completeMaintenanceTask',
      'createTimeEntry',
      'recordMeterReading',
      'recordPayment',
      'recordPresence',
      'reportMaintenance',
      'saveHandoverChecklist',
      'startTimer',
      'stopTimer',
      'updateTaskProgress',
      'uploadHandoverPhoto',
      'uploadMaintenancePhoto',
      'uploadMeterReadingPhoto',
      'uploadPresencePhoto',
      'uploadTaskPhoto',
    ]);
  });

  it('matches the query keys of queryKeys.ts for today and tomorrow only', () => {
    const today = localDate();
    const tomorrow = localDate(1);
    const yes = [
      queryKeys.me,
      queryKeys.maps(M),
      queryKeys.map(M, 'x'),
      queryKeys.dayView(M, 'x', today),
      queryKeys.dayView(M, 'x', 'today'),
      queryKeys.dayView(M, 'x', tomorrow),
      queryKeys.movements(M, today),
      queryKeys.movements(M, tomorrow, 'x'),
      queryKeys.presences(M, today),
      queryKeys.mapPayments(M, 'x', today),
      queryKeys.collectibles(M, today),
      [...queryKeys.cashSession(M, 'current'), 'r1'],
      queryKeys.deadlines(M, { due_before: '2027-01-01' }),
      queryKeys.maintenanceWorklist(M),
      queryKeys.utilityMeters(M, { route: 'dock' }),
    ];
    const no = [
      queryKeys.dayView(M, 'x', localDate(2)),
      queryKeys.movements(M, localDate(-3)),
      queryKeys.presences(M, tomorrow),
      queryKeys.berths(M),
      queryKeys.cashSession(M, 's1'),
      queryKeys.customers(M),
      queryKeys.charges(M),
      queryKeys.audit(M),
      queryKeys.webhooks(M),
      queryKeys.devices(M),
      queryKeys.apiKeys(M),
      queryKeys.desktop.status,
      queryKeys.bridge.status,
      queryKeys.transitQr(M, 'c1'),
      queryKeys.chargeExports(M),
      queryKeys.maintenanceTasks(M),
      queryKeys.maintenanceTask(M, 't1'),
      queryKeys.utilityMeters(M),
      queryKeys.utilityMeters(M, { kind: 'power' }),
      queryKeys.meterReadings(M),
      queryKeys.utilityConsumptions(M),
    ];
    for (const k of yes) expect(isPersistable(k, M), JSON.stringify(k)).toBe(true);
    for (const k of no) expect(isPersistable(k, M), JSON.stringify(k)).toBe(false);
    expect(isPersistable(queryKeys.maps('other'), M)).toBe(false);
  });

  it('fills the assigned activities slot with the maintenance worklist (M9) and keeps the dock meters', () => {
    const slot = OFFLINE_QUERY_ALLOWLIST.find((e) => e.name === 'activities.assigned');
    expect(slot?.operationId).toBe('getMaintenanceWorklist');
    expect(slot?.match(queryKeys.maintenanceWorklist(M))).toBe(true);
    expect(slot?.match(['marina', M, 'anything'])).toBe(false);
    const meters = OFFLINE_QUERY_ALLOWLIST.find((e) => e.name === 'meters.dock');
    expect(meters?.operationId).toBe('listUtilityMeters');
    expect(OFFLINE_QUERY_ALLOWLIST.every((e) => e.operationId !== null)).toBe(true);
  });

  it('caches the yard work list and the lifting day of today and tomorrow (M10), nothing else of the yard', () => {
    const my = OFFLINE_QUERY_ALLOWLIST.find((e) => e.name === 'activities.yard');
    expect(my?.operationId).toBe('getMyYardWork');
    const lift = OFFLINE_QUERY_ALLOWLIST.find((e) => e.name === 'activities.lifting');
    expect(lift?.operationId).toBe('listEquipmentBookings');
    const yes = [
      queryKeys.myYardWork(M),
      queryKeys.equipmentBookings(M, 'today'),
      queryKeys.equipmentBookings(M, localDate()),
      queryKeys.equipmentBookings(M, localDate(1)),
    ];
    const no = [
      queryKeys.equipmentBookings(M, localDate(2)),
      queryKeys.equipmentBookings(M, 'today', { work_order_id: 'o1' }),
      queryKeys.planner(M, '2027-06-01', '2027-06-07'),
      queryKeys.employees(M),
      queryKeys.yardLoad(M),
      queryKeys.atRiskOrders(M),
      queryKeys.myYardWork('other'),
    ];
    for (const k of yes) expect(isPersistable(k, M), JSON.stringify(k)).toBe(true);
    for (const k of no) expect(isPersistable(k, M), JSON.stringify(k)).toBe(false);
  });

  it('enables both M10 offline kinds: no reserved kind is left for M10', () => {
    const kinds = new Set(Object.values(OFFLINE_OPERATION_KINDS));
    expect(kinds.has('yard.timesheet')).toBe(true);
    expect(kinds.has('yard.activity_done')).toBe(true);
  });
  it('caches the rental phone slot (M11): day of today and tomorrow, bookings, settings and handover drafts', () => {
    const yes = [
      queryKeys.rentalDay(M, 'today'),
      queryKeys.rentalDay(M, localDate()),
      queryKeys.rentalDay(M, localDate(1)),
      queryKeys.rentalBooking(M, 'b1'),
      queryKeys.rentalSettings(M),
      queryKeys.rentalHandover(M, 'b1', 'checkout'),
      queryKeys.rentalHandover(M, 'b1', 'checkin'),
    ];
    const no = [
      queryKeys.rentalDay(M, localDate(2)),
      queryKeys.rentalPlanner(M, '2027-06-01', '2027-06-07', 'hours'),
      queryKeys.rentalBookings(M),
      queryKeys.rentalUnits(M),
      queryKeys.rentalDeposit(M, 'b1'),
      queryKeys.rentalBooking('other', 'b1'),
    ];
    for (const k of yes) expect(isPersistable(k, M), JSON.stringify(k)).toBe(true);
    for (const k of no) expect(isPersistable(k, M), JSON.stringify(k)).toBe(false);
    const kinds = new Set(Object.values(OFFLINE_OPERATION_KINDS));
    expect(kinds.has('rental.checklist')).toBe(true);
    expect(OFFLINE_QUERY_ALLOWLIST.every((e) => e.operationId !== null)).toBe(true);
  });
});
