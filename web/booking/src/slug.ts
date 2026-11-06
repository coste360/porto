/** Marina slug from a `/b/{slug}` path (the booking app has no router in M0). */
export function slugFromPath(pathname: string): string | null {
  const match = /^\/b\/([a-z0-9-]{3,63})\/?$/.exec(pathname);
  return match?.[1] ?? null;
}
