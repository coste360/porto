/** Reads `#token=…&invite=1` style parameters (tokens never travel in the query string). */
export function readHashParams(hash: string): URLSearchParams {
  return new URLSearchParams(hash.startsWith('#') ? hash.slice(1) : hash);
}

/** Only same-app relative paths are accepted as post-login redirects. */
export function safeRedirect(target: unknown): string {
  if (typeof target !== 'string') return '/';
  if (!target.startsWith('/') || target.startsWith('//') || target.includes('\\')) return '/';
  return target;
}
