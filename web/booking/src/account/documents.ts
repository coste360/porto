import {
  BOOKING_DOCUMENT_MAX_BYTES,
  BOOKING_DOCUMENT_TYPES,
  type BookingDocumentType,
} from '@porto/shared/api';

export const ACCOUNT_DOCUMENT_TYPES: BookingDocumentType[] = [
  'insurance_policy',
  'boating_licence',
  'navigation_licence',
  'registration_certificate',
  'identity_card',
];

/** Checks a file before upload: PDF, PNG or JPEG up to 10 MB. Returns the i18n key of the problem, or null. */
export function fileProblem(file: File): string | null {
  if (!(BOOKING_DOCUMENT_TYPES as readonly string[]).includes(file.type))
    return 'account:documents.badType';
  if (file.size > BOOKING_DOCUMENT_MAX_BYTES) return 'account:documents.tooLarge';
  return null;
}
