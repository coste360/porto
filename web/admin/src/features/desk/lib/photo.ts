import { PRESENCE_PHOTO_MAX_BYTES, PRESENCE_PHOTO_TYPES } from '@porto/shared/api';

/** Photo check before upload: type and size (the server checks them again). */
export function photoError(file: File | null): string | null {
  if (!file) return null;
  if (!(PRESENCE_PHOTO_TYPES as readonly string[]).includes(file.type))
    return 'desk:presence.photoType';
  if (file.size > PRESENCE_PHOTO_MAX_BYTES) return 'desk:presence.photoSize';
  return null;
}
