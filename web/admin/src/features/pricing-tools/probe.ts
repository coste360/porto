import type { PricingSimulateRequest } from '@porto/shared/api';

import { CONTRACT_TYPES } from './lib/enums';

// A probe is a simulate request carried in the URL (`?probe=`): the matrix links each cell to the simulator with
// the exact input the server priced, so the simulator reaches the identical engine call (INV-23).

const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/;

function toBase64Url(bytes: Uint8Array): string {
  let binary = '';
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function fromBase64Url(text: string): Uint8Array | null {
  if (!/^[A-Za-z0-9_-]*$/.test(text)) return null;
  const padded = text.replace(/-/g, '+').replace(/_/g, '/');
  try {
    const binary = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4));
    return Uint8Array.from(binary, (c) => c.charCodeAt(0));
  } catch {
    return null;
  }
}

/** Simulate request → base64url JSON for `?probe=`. */
export function encodeProbe(p: PricingSimulateRequest): string {
  return toBase64Url(new TextEncoder().encode(JSON.stringify(p)));
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

const isInt = (v: unknown) => typeof v === 'number' && Number.isSafeInteger(v);

/** Every number in the probe must be a safe integer (cents, cm, bp, milli): floats are refused. */
function onlyIntegers(v: unknown): boolean {
  if (typeof v === 'number') return isInt(v);
  if (Array.isArray(v)) return v.every(onlyIntegers);
  if (isRecord(v)) return Object.values(v).every(onlyIntegers);
  return true;
}

/** `?probe=` value → simulate request, or null when it is not a well-formed probe. */
export function decodeProbe(s: string): PricingSimulateRequest | null {
  const bytes = fromBase64Url(s);
  if (!bytes) return null;
  let value: unknown;
  try {
    value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
  } catch {
    return null;
  }
  if (!isRecord(value) || !onlyIntegers(value)) return null;
  const { contract_type, arrival, departure } = value;
  if (!(CONTRACT_TYPES as readonly unknown[]).includes(contract_type)) return null;
  if (typeof arrival !== 'string' || !ISO_DATE.test(arrival)) return null;
  if (typeof departure !== 'string' || !ISO_DATE.test(departure)) return null;
  return value as PricingSimulateRequest;
}
