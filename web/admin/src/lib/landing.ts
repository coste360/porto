import type { Device, Permission } from '@porto/shared/api';

/**
 * Where a phone lands (m6b-plan §6.1, spec 31.3): after pairing (device kind `mobile`) and after sign-in on a
 * viewport narrower than 768 px (DashboardPage passes `mobile`). Desktops always open the marina overview. Paths
 * are relative to the admin base (`/admin`).
 *
 * | Permissions (first match)                         | Roles today                      | Phone landing        |
 * |---------------------------------------------------|----------------------------------|----------------------|
 * | map.view with transits.desk                       | owner, director, office, dock    | /m/<id>/dock         |
 * | yard.timesheets or yard.manage (M10)              | yard_mgr, yard                   | /m/<id>/yard-phone   |
 * | presence.write (haul-out and launch without desk) | (none today: yard_mgr lands above) | /m/<id>/dock       |
 * | storage.manage (M9)                               | (none today)                     | /m/<id>/storage-phone |
 * | rental.manage without presence.write (M11)        | rental                           | /m/<id>/rental-phone |
 * | maintenance.report (M9)                           | (none today: rental lands above) | /m/<id>/maintenance-phone |
 * | reports.all or marina.configure                   | accounting, viewer               | /m/<id>/direction    |
 * | anything else                                     | (none today)                     | /m/<id>              |
 *
 * Later milestones add their row above the fallback, before the direction row when the role also holds
 * reports.all.
 * Owners and directors hold presence.write and land on the dock; the dock's "Altro" tab links the direction
 * screen for them.
 */
export function landingFor(
  permissions: readonly Permission[],
  kind: Device['kind'],
  marinaId: string,
): string {
  const id = encodeURIComponent(marinaId);
  const base = `/m/${id}`;
  if (kind !== 'mobile') return base;
  const has = (p: Permission) => permissions.includes(p);
  if (has('map.view') && has('transits.desk')) return `${base}/dock`;
  // yard_mgr holds presence.write (haul-out and launch) but lands on the yard screen.
  if (has('yard.timesheets') || has('yard.manage')) return `${base}/yard-phone`;
  if (has('presence.write')) return `${base}/dock`;
  if (has('storage.manage')) return `${base}/storage-phone`;
  if (has('rental.manage')) return `${base}/rental-phone`;
  if (has('maintenance.report')) return `${base}/maintenance-phone`;
  if (has('reports.all') || has('marina.configure')) return `${base}/direction`;
  return base;
}

/** Whether a user may open the direction phone screen (dashboard, map, deadlines). */
export function canUseDirection(permissions: readonly Permission[]): boolean {
  return permissions.includes('reports.all') || permissions.includes('marina.configure');
}

/** Whether a user may open the storage phone screen (movements, accessibility warnings; M9). */
export function canUseStoragePhone(permissions: readonly Permission[]): boolean {
  return permissions.includes('storage.read');
}

/** Whether a user may open the maintenance phone screen (worklist, report, outcome; M9). */
export function canUseMaintenancePhone(permissions: readonly Permission[]): boolean {
  return permissions.includes('maintenance.report') || permissions.includes('maintenance.manage');
}

/** Whether a user may open the yard phone screen (my tasks, timer, hours, lifting day; M10). */
export function canUseYardPhone(permissions: readonly Permission[]): boolean {
  return permissions.includes('yard.timesheets') || permissions.includes('yard.manage');
}

/** Whether a user may open the rental phone screen (today, check-out, check-in, quick cash; M11). */
export function canUseRentalPhone(permissions: readonly Permission[]): boolean {
  return permissions.includes('rental.manage');
}

/** Whether a user may open the dock phone screen. */
export function canUseDock(permissions: readonly Permission[]): boolean {
  return permissions.includes('presence.write') || permissions.includes('map.view');
}
