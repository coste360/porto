import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { MAIL_DIR } from './env';

function decodeQuotedPrintable(body: string): string {
  const bytes = body
    .replace(/=\r?\n/g, '')
    .replace(/=([0-9A-F]{2})/gi, (_, hex: string) => String.fromCharCode(parseInt(hex, 16)));
  return Buffer.from(bytes, 'latin1').toString('utf8');
}

/** Decoded text of every .eml written by the file transport whose headers mention `to`. */
function messagesTo(to: string): string[] {
  return readdirSync(MAIL_DIR)
    .filter((f) => f.endsWith('.eml'))
    .sort()
    .map((f) => readFileSync(path.join(MAIL_DIR, f), 'utf8'))
    .filter((raw) => {
      const headerEnd = raw.search(/\r?\n\r?\n/);
      return raw.slice(0, headerEnd).toLowerCase().includes(to.toLowerCase());
    })
    .map(decodeQuotedPrintable);
}

/** Waits for a message to `to` containing a reset/invitation link and returns the link (newest first). */
export async function waitForResetLink(to: string, timeoutMs = 15_000): Promise<string> {
  const deadline = Date.now() + timeoutMs;
  const seen = messagesTo(to).length;
  for (;;) {
    const all = messagesTo(to);
    for (const text of all.slice(seen).reverse()) {
      const m = text.match(/https?:\/\/[^\s"'<>]+\/admin\/reset-password#token=[^\s"'<>]+/);
      if (m) return m[0].replace(/&amp;/g, '&');
    }
    if (Date.now() > deadline) throw new Error(`no reset link for ${to} in ${MAIL_DIR}`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

/** Number of messages already delivered to `to` (call before the action, pass to waitForResetLink). */
export function countMessagesTo(to: string): number {
  return messagesTo(to).length;
}
