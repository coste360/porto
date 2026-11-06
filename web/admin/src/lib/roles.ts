import type { Permission, Role } from '@porto/shared/api';

/** Roles of spec 26.1, in display order. */
export const ROLES: readonly Role[] = [
  'owner',
  'director',
  'office',
  'accounting',
  'dock',
  'yard_mgr',
  'yard',
  'rental',
  'viewer',
];

/** Scopes an API key may never carry (plan §3). */
export const API_KEY_FORBIDDEN_SCOPES: readonly Permission[] = [
  'users.manage',
  'organization.manage',
];

/** Scopes the current user may grant to a new API key: a subset of their own permissions. */
export function grantableScopes(own: readonly Permission[]): Permission[] {
  return own.filter((p) => !API_KEY_FORBIDDEN_SCOPES.includes(p));
}
