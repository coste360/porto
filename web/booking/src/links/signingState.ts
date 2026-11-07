// State of the customer signing page (spec 15.4, m4-plan §6.5, D13): read the PDF, confirm the identity data,
// request an email code, type it (attempts left), and the final pages. The view state is derived from the link
// data and the last API answer only: the page keeps no secret and never stores the code.
import { isProblem, type PublicSignature, type PublicSignatureResult } from '@porto/shared/api';

export type SigningView =
  | { kind: 'confirm' }
  | { kind: 'code'; attemptsLeft: number | null; invalid: boolean }
  | { kind: 'locked' }
  | { kind: 'rateLimited'; retryAfterSeconds: number | null }
  | { kind: 'signed'; deposit?: PublicSignature['deposit'] }
  | { kind: 'declined' }
  | { kind: 'closed'; status: PublicSignature['request_status'] }
  | { kind: 'waiting' };

export type SigningInput = {
  link: Pick<PublicSignature, 'signer_status' | 'request_status' | 'attempts_left' | 'deposit'>;
  /** A code was requested in this page session (OTP sent). */
  codeSent: boolean;
  lastResult?: PublicSignatureResult | null;
  lastError?: unknown;
};

const CLOSED: readonly PublicSignature['request_status'][] = ['expired', 'cancelled', 'declined'];

export function signingView(i: SigningInput): SigningView {
  const signer = i.lastResult?.signer_status ?? i.link.signer_status;
  const request = i.lastResult?.request_status ?? i.link.request_status;
  if (signer === 'signed') {
    return { kind: 'signed', deposit: i.lastResult?.deposit ?? i.link.deposit };
  }
  if (signer === 'declined') return { kind: 'declined' };
  if (CLOSED.includes(request)) return { kind: 'closed', status: request };
  if (signer === 'waiting') return { kind: 'waiting' };
  if (isProblem(i.lastError, 'otp_locked')) return { kind: 'locked' };
  if (isProblem(i.lastError, 'otp_rate_limited')) {
    return { kind: 'rateLimited', retryAfterSeconds: retryAfter(i.lastError) };
  }
  if (!i.codeSent) return { kind: 'confirm' };
  const invalid = isProblem(i.lastError, 'otp_invalid');
  const fromError = invalid ? attemptsFromProblem(i.lastError) : null;
  return {
    kind: 'code',
    invalid,
    attemptsLeft: fromError ?? i.lastResult?.attempts_left ?? i.link.attempts_left ?? null,
  };
}

function attemptsFromProblem(e: unknown): number | null {
  if (!isProblem(e)) return null;
  const d = e.details as { attempts_left?: unknown } | undefined;
  return typeof d?.attempts_left === 'number' ? d.attempts_left : null;
}

function retryAfter(e: unknown): number | null {
  return isProblem(e) && typeof e.retryAfterSeconds === 'number' ? e.retryAfterSeconds : null;
}

/** Six digits exactly (D13). */
export const OTP_LENGTH = 6;
export function codeComplete(code: string): boolean {
  return new RegExp(`^\\d{${OTP_LENGTH}}$`).test(code);
}
