import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import type { APIRequestContext } from '@playwright/test';

import { MAIL_DIR } from './env';

// M4 customer-link helpers for the Playwright specs (m4-plan §9.5; signatures fixed). They read the emails of the
// dev mail directory (the file transport of M0, as mail.ts) as the customer would. `request` is kept in the
// signatures for specs that run against a remote mail catcher later; the file transport needs no request.

export type LinkPurpose = 'quote' | 'renewal' | 'signature' | 'unsubscribe';

function decodeQuotedPrintable(body: string): string {
  const bytes = body
    .replace(/=\r?\n/g, '')
    .replace(/=([0-9A-F]{2})/gi, (_, hex: string) => String.fromCharCode(parseInt(hex, 16)));
  return Buffer.from(bytes, 'latin1').toString('utf8');
}

/** Decoded text of every .eml whose headers mention `to`, oldest first. */
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

async function poll<T>(what: string, find: () => T | null, timeoutMs: number): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const found = find();
    if (found !== null) return found;
    if (Date.now() > deadline) throw new Error(`${what}: nothing found in ${MAIL_DIR}`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

/** The magic links (`/b/{slug}/m/{purpose}/{token}`) of `purpose` in a message, in order. */
export function linksIn(text: string, purpose: LinkPurpose): string[] {
  const re = new RegExp(`https?://[^\\s"'<>]+/b/[a-z0-9-]+/m/${purpose}/[A-Za-z0-9_-]+`, 'g');
  return (text.match(re) ?? []).map((l) => l.replace(/&amp;/g, '&'));
}

/** The latest magic link of `purpose` emailed to `to`. */
export async function latestLinkFromMail(
  request: APIRequestContext,
  to: string,
  purpose: LinkPurpose,
  timeoutMs = 20_000,
): Promise<string> {
  void request;
  return poll(
    `latestLinkFromMail(${purpose})`,
    () => {
      const all = messagesTo(to);
      for (let i = all.length - 1; i >= 0; i--) {
        const links = linksIn(all[i]!, purpose);
        if (links.length) return links[links.length - 1]!;
      }
      return null;
    },
    timeoutMs,
  );
}

/** The 6-digit code of an OTP message (D13), or null. */
export function otpIn(text: string): string | null {
  const m = text.match(/(?:codice|code)[^0-9]{0,80}(\d{6})(?!\d)/i);
  return m ? m[1]! : null;
}

/** The latest signature OTP code emailed to `to`. */
export async function latestOtpFromMail(
  request: APIRequestContext,
  to: string,
  timeoutMs = 20_000,
): Promise<string> {
  void request;
  return poll(
    'latestOtpFromMail',
    () => {
      const all = messagesTo(to);
      for (let i = all.length - 1; i >= 0; i--) {
        const code = otpIn(all[i]!);
        if (code) return code;
      }
      return null;
    },
    timeoutMs,
  );
}

/** Path of a link (the spec navigates with the base URL of the run). */
export function pathOf(link: string): string {
  const u = new URL(link);
  return u.pathname + u.search;
}
