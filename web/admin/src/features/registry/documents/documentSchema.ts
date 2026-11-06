import { z } from 'zod';

import {
  DOCUMENT_FILE_MAX_BYTES,
  DOCUMENT_FILE_TYPES,
  type Document,
  type DocumentInput,
  type DocumentPatch,
} from '@porto/shared/api';

import { DOCUMENT_TYPES } from '../lib/enums';

const isoDate = z.union([z.literal(''), z.iso.date('documents:validation.date')]);

export const documentSchema = z
  .object({
    type: z.enum(DOCUMENT_TYPES),
    number: z.string().trim().max(100),
    issuer: z.string().trim().max(200),
    issued_on: isoDate,
    expires_on: isoDate,
    notes: z.string().max(2000),
  })
  .refine((v) => !v.issued_on || !v.expires_on || v.issued_on <= v.expires_on, {
    path: ['expires_on'],
    message: 'documents:validation.expiryBeforeIssue',
  });

export type DocumentFormValues = z.infer<typeof documentSchema>;

export function documentToValues(d?: Document, type?: Document['type']): DocumentFormValues {
  return {
    type: d?.type ?? type ?? 'other',
    number: d?.number ?? '',
    issuer: d?.issuer ?? '',
    issued_on: d?.issued_on ?? '',
    expires_on: d?.expires_on ?? '',
    notes: d?.notes ?? '',
  };
}

export function valuesToDocumentInput(
  v: DocumentFormValues,
  owner: { type: 'customer' | 'vessel'; id: string },
): DocumentInput {
  return {
    owner_type: owner.type,
    ...(owner.type === 'customer' ? { customer_id: owner.id } : { vessel_id: owner.id }),
    type: v.type,
    ...(v.number ? { number: v.number } : {}),
    ...(v.issuer ? { issuer: v.issuer } : {}),
    ...(v.issued_on ? { issued_on: v.issued_on } : {}),
    ...(v.expires_on ? { expires_on: v.expires_on } : {}),
    ...(v.notes ? { notes: v.notes } : {}),
  };
}

export function valuesToDocumentPatch(v: DocumentFormValues): DocumentPatch {
  return {
    type: v.type,
    number: v.number || null,
    issuer: v.issuer || null,
    issued_on: v.issued_on || null,
    expires_on: v.expires_on || null,
    notes: v.notes,
  };
}

export type FileCheck = 'ok' | 'too_large' | 'type' | 'empty';

/** Client-side check before the upload (the server sniffs the content again, D19). */
export function checkDocumentFile(file: File): FileCheck {
  if (file.size === 0) return 'empty';
  if (file.size > DOCUMENT_FILE_MAX_BYTES) return 'too_large';
  if (!(DOCUMENT_FILE_TYPES as readonly string[]).includes(file.type)) return 'type';
  return 'ok';
}
