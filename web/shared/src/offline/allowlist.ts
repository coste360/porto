// Offline allowlists (m6b-plan §4.2, spec 29.2). OFFLINE_OPERATIONS must equal the operations flagged
// `x-porto-offline: true` in api/openapi.yaml (the server pins the same set, TestPolicies_OfflineSet).

/** Kinds of queued operations. M9 enables utility.reading, maintenance.report and maintenance.outcome
 * (m9-plan §10); reserved for M10–M11 (not enabled): yard.timesheet, yard.activity_done, rental.checklist. */
export type OfflineOpKind =
  | 'presence.record'
  | 'presence.photo'
  | 'payment.cash'
  | 'utility.reading'
  | 'maintenance.report'
  | 'maintenance.outcome';

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
  // Assigned activities (M9: the maintenance worklist; M10–M11 add yard and rentals):
  // ['marina', id, 'maintenance', 'worklist']
  {
    name: 'activities.assigned',
    operationId: 'getMaintenanceWorklist',
    match: (k) => marinaArea('maintenance', 'worklist')(k) && k.length === 4,
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
