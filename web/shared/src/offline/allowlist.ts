// Offline allowlists (m6b-plan §4.2, spec 29.2). OFFLINE_OPERATIONS must equal the operations flagged
// `x-porto-offline: true` in api/openapi.yaml (the server pins the same set, TestPolicies_OfflineSet).

/** Kinds of queued operations. M9 enables utility.reading, maintenance.report and maintenance.outcome
 * (m9-plan §10); M10 enables yard.timesheet and yard.activity_done (m10-plan §7); M11 enables
 * rental.checklist (m11-plan §12). */
export type OfflineOpKind =
  | 'presence.record'
  | 'presence.photo'
  | 'payment.cash'
  | 'utility.reading'
  | 'maintenance.report'
  | 'maintenance.outcome'
  | 'yard.timesheet'
  | 'yard.activity_done'
  | 'rental.checklist';

/** The only operations the queue may replay, with their kind. */
export const OFFLINE_OPERATION_KINDS = {
  recordPresence: 'presence.record',
  uploadPresencePhoto: 'presence.photo',
  recordPayment: 'payment.cash',
  // M9 (m9-plan §10): the photo uploads depend on their reading or report (dependsOn binds readingId / taskId).
  recordMeterReading: 'utility.reading',
  uploadMeterReadingPhoto: 'utility.reading',
  reportMaintenance: 'maintenance.report',
  uploadMaintenancePhoto: 'maintenance.report',
  completeMaintenanceTask: 'maintenance.outcome',
  // M10 (m10-plan §7): stopTimer depends on startTimer (dependsOn binds timesheetId), uploadTaskPhoto on its task
  // (taskId); none creates, moves or releases allocations or bookings (INV-71).
  startTimer: 'yard.timesheet',
  stopTimer: 'yard.timesheet',
  createTimeEntry: 'yard.timesheet',
  updateTaskProgress: 'yard.activity_done',
  uploadTaskPhoto: 'yard.activity_done',
  // M11 (m11-plan §12): checklist, fuel, engine hours and boat photos of a confirmed or checked-out rental;
  // uploadHandoverPhoto depends on its checklist save; neither changes status, slots, charges or deposits (INV-71).
  saveHandoverChecklist: 'rental.checklist',
  uploadHandoverPhoto: 'rental.checklist',
} as const satisfies Record<string, OfflineOpKind>;

export const OFFLINE_OPERATIONS: readonly string[] = Object.keys(OFFLINE_OPERATION_KINDS);

/** A cached query: the operation and a matcher on TanStack query keys from queryKeys.ts. */
export type AllowlistEntry = {
  /** Allowlist slot name (stable, used by tests and the persister). */
  name: string;
  /** operationId of the query, or null for a reserved slot. */
  operationId: string | null;
  /** True when a query key (already scoped to the current marina) belongs to this entry. */
  match: (key: readonly unknown[]) => boolean;
};

const at = (key: readonly unknown[], i: number) => key[i];
const marinaArea = (area: string, sub?: string) => (key: readonly unknown[]) =>
  at(key, 0) === 'marina' && at(key, 2) === area && (sub === undefined || at(key, 3) === sub);

/** Local calendar date of the device, YYYY-MM-DD, `offset` days from now. The marina time zone is not known
 * here; a device a day off only caches one day less. */
export function localDate(offset = 0, now: Date = new Date()): string {
  const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + offset);
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  return `${d.getFullYear()}-${mm}-${dd}`;
}

/** Dates kept for the day screens: today and tomorrow ('today' is the alias of the day view). */
function inWindow(date: unknown, days: number): boolean {
  if (date === undefined || date === 'today') return true;
  for (let i = 0; i < days; i++) if (date === localDate(i)) return true;
  return false;
}

/** Queries persisted for 48 hours, current user and marina only (m6b-plan §4.2). Keys from queryKeys.ts. */
export const OFFLINE_QUERY_ALLOWLIST: readonly AllowlistEntry[] = [
  { name: 'me', operationId: 'getMe', match: (k) => k.length === 1 && k[0] === 'me' },
  { name: 'meta', operationId: 'getMeta', match: (k) => k.length === 1 && k[0] === 'meta' },
  { name: 'maps', operationId: 'listMaps', match: (k) => marinaArea('maps')(k) && k.length === 3 },
  { name: 'map', operationId: 'getMap', match: marinaArea('maps', 'detail') },
  // ['marina', id, 'ops', 'day', mapId, date]
  {
    name: 'mapDay',
    operationId: 'getMapDay',
    match: (k) => marinaArea('ops', 'day')(k) && inWindow(k[5], 2),
  },
  // ['marina', id, 'desk', 'movements', date, mapId?]
  {
    name: 'movements',
    operationId: 'getMovements',
    match: (k) => marinaArea('desk', 'movements')(k) && inWindow(k[4], 2),
  },
  {
    name: 'presences',
    operationId: 'listPresences',
    match: (k) => marinaArea('desk', 'presences')(k) && inWindow(k[4], 1),
  },
  // ['marina', id, 'billing', 'map-payments', mapId, date]
  {
    name: 'paymentStatus',
    operationId: 'getMapPaymentStatus',
    match: (k) => marinaArea('billing', 'map-payments')(k) && inWindow(k[5], 2),
  },
  {
    name: 'collectibles',
    operationId: 'listCollectibles',
    match: (k) => marinaArea('billing', 'collectibles')(k) && inWindow(k[4], 1),
  },
  // ['marina', id, 'billing', 'cash-session', 'current', registerId]
  {
    name: 'cashSession',
    operationId: 'getCurrentCashSession',
    match: (k) => marinaArea('billing', 'cash-session')(k) && k[4] === 'current',
  },
  { name: 'deadlines', operationId: 'listDeadlines', match: marinaArea('registry', 'deadlines') },
  // Assigned activities (M9: the maintenance worklist; M10 the yard work list below; M11 adds rentals):
  // ['marina', id, 'maintenance', 'worklist']
  {
    name: 'activities.assigned',
    operationId: 'getMaintenanceWorklist',
    match: (k) => marinaArea('maintenance', 'worklist')(k) && k.length === 4,
  },
  // M10 (m10-plan §7, §14.3): the yard phone's work list (assignments ±7 days and the running timer) and the
  // lifting day calendar of today and tomorrow (read only on the phone):
  // ['marina', id, 'yard', 'my-work'] and ['marina', id, 'yard', 'lifting', date]
  {
    name: 'activities.yard',
    operationId: 'getMyYardWork',
    match: (k) => marinaArea('yard', 'my-work')(k) && k.length === 4,
  },
  {
    name: 'activities.lifting',
    operationId: 'listEquipmentBookings',
    match: (k) => marinaArea('yard', 'lifting')(k) && k.length === 5 && inWindow(k[4], 2),
  },
  // M11 rental slot (m11-plan §12): the rental phone's day view of today and tomorrow, the bookings opened from
  // it, the settings (checklist template) and the handover drafts:
  // ['marina', id, 'rental', 'day', date], ['marina', id, 'rental', 'booking', bookingId],
  // ['marina', id, 'rental', 'settings'], ['marina', id, 'rental', 'handover', bookingId, phase]
  {
    name: 'rental.day',
    operationId: 'getRentalDay',
    match: (k) => marinaArea('rental', 'day')(k) && k.length === 5 && inWindow(k[4], 2),
  },
  {
    name: 'rental.booking',
    operationId: 'getRentalBooking',
    match: (k) => marinaArea('rental', 'booking')(k) && k.length === 5,
  },
  {
    name: 'rental.settings',
    operationId: 'getRentalSettings',
    match: (k) => marinaArea('rental', 'settings')(k) && k.length === 4,
  },
  {
    name: 'rental.handover',
    operationId: 'getHandover',
    match: (k) => marinaArea('rental', 'handover')(k) && k.length === 6,
  },
  // The meters of the dock reading round (m9-plan §10): ['marina', id, 'utilities', 'meters', { route: 'dock' }]
  {
    name: 'meters.dock',
    operationId: 'listUtilityMeters',
    match: (k) => marinaArea('utilities', 'meters')(k) && isDockRoute(k[4]),
  },
];

function isDockRoute(filters: unknown): boolean {
  return (
    typeof filters === 'object' &&
    filters !== null &&
    (filters as { route?: unknown }).route === 'dock'
  );
}

/** True when a query key may be persisted for the given marina (other marinas never are). */
export function isPersistable(key: readonly unknown[], marinaId: string): boolean {
  if (key[0] === 'marina' && key[1] !== marinaId) return false;
  return OFFLINE_QUERY_ALLOWLIST.some((e) => e.match(key));
}
