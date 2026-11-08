// Pure helpers of the remote pairing form (m6b-plan §7.2).
import { z } from 'zod';

import type { DesktopRemotePairRequest } from '@porto/shared/desktop/hooks';

export const remotePairSchema = z
  .object({
    link: z.string().trim(),
    url: z.string().trim(),
    code: z.string().trim(),
    fp: z.string().trim(),
    name: z.string().trim().max(100),
    pin: z.string().regex(/^[0-9]{6,12}$/, 'desktop:pin.invalid'),
    confirm: z.string(),
  })
  .refine((v) => v.pin === v.confirm, { path: ['confirm'], message: 'desktop:pin.mismatch' })
  .refine((v) => v.link.startsWith('porto://') || (v.url !== '' && v.code !== ''), {
    path: ['link'],
    message: 'common:validation.required',
  });
export type RemotePairForm = z.infer<typeof remotePairSchema>;

/** The request of a valid form: the link wins over address and code. */
export function toPairRequest(token: string, v: RemotePairForm): DesktopRemotePairRequest {
  const base = { first_run_token: token, pin: v.pin, ...(v.name ? { name: v.name } : {}) };
  if (v.link) return { ...base, link: v.link };
  return { ...base, url: v.url, code: v.code, ...(v.fp ? { fp: v.fp } : {}) };
}
