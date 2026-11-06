/** Reads a cookie by name from `document.cookie` (undefined when missing). */
export function readCookie(name: string, source: string = document.cookie): string | undefined {
  for (const part of source.split(';')) {
    const eq = part.indexOf('=');
    if (eq < 0) continue;
    if (part.slice(0, eq).trim() === name) {
      const raw = part.slice(eq + 1).trim();
      try {
        return decodeURIComponent(raw);
      } catch {
        return raw;
      }
    }
  }
  return undefined;
}
