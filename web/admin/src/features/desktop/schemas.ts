import { z } from 'zod';

import { emailField, newPasswordField, requiredString } from '../../lib/schemas';

/** First-run owner form (m6a-plan §4.4); messages are i18n keys. */
export const firstRunSchema = z
  .object({
    organization: requiredString(),
    name: requiredString(),
    email: emailField(),
    password: newPasswordField(),
    confirm: z.string(),
    update_check: z.boolean(),
  })
  .refine((v) => v.password === v.confirm, {
    path: ['confirm'],
    message: 'common:validation.passwordMismatch',
  });
export type FirstRunForm = z.infer<typeof firstRunSchema>;
