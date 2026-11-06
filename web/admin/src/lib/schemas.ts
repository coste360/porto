import { z } from 'zod';

import type { Role } from '@porto/shared/api';

import { ROLES } from './roles';

/** zod messages are i18n keys, rendered by FormField. */
export const requiredString = () => z.string().trim().min(1, 'common:validation.required');
export const emailField = () =>
  z.string().trim().min(1, 'common:validation.required').pipe(z.email('common:validation.email'));
/** Password policy shown to the user (the server checks it again: weak_password). */
export const newPasswordField = () => z.string().min(12, 'common:validation.passwordMin');
export const totpCodeField = () => z.string().regex(/^\d{6}$/, 'common:validation.code');
export const slugField = () =>
  z.string().regex(/^[a-z0-9](?:[a-z0-9-]{1,61})[a-z0-9]$/, 'common:validation.slug');
export const httpsUrlField = () =>
  z
    .string()
    .trim()
    .refine((value) => {
      try {
        return new URL(value).protocol === 'https:';
      } catch {
        return false;
      }
    }, 'common:validation.url');

/** Add-member form (team page). */
export const addMemberSchema = z.object({
  name: requiredString(),
  email: emailField(),
  role: z.enum(ROLES as [Role, ...Role[]], 'common:validation.required'),
});
