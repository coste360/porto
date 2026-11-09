import { profilePhotoUrl, type PublicProfile } from '@porto/shared/api';

// Helpers of the public profile page (no components: fast refresh keeps the component files pure).

/** The preview of the draft: /p/{slug}?preview=1&marina={id} opened from the panel (session cookie, never cached). */
export function previewMarinaId(search: string): string | null {
  const q = new URLSearchParams(search);
  return q.get('preview') ? q.get('marina') : null;
}

/** Photo URLs of the draft go through the panel endpoint (unpublished photos are not public). */
export function photoSrc(marinaId: string | null): (url: string) => string {
  if (!marinaId) return (url) => url;
  return (url) => profilePhotoUrl(marinaId, url.slice(url.lastIndexOf('/') + 1));
}

/** True when the profile has something to show in the hours section. */
export function hasHours(p: PublicProfile): boolean {
  return (
    (p.office_hours ?? []).length > 0 || (p.dock_hours ?? []).length > 0 || Boolean(p.hours_note)
  );
}
