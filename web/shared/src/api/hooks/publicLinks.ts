// Query and mutation hooks of the customer magic-link pages (m4-plan §8.2, §7.3.6) used by the booking SPA:
// resolve a link, fetch its PDF, accept a quote (with missing data and document uploads), accept or decline a
// renewal, sign with an email OTP and unsubscribe. The token is the only credential: keys live under
// ['public', slug, 'link', token], never under a marina, and nothing is cached across tokens.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { unwrap } from '../client';
import { useApi } from '../context';
import { ProblemError } from '../problem';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

const L = '/api/public/v1/{slug}/links/{token}' as const;

export type PublicLink = S['PublicLink'];
export type PublicQuote = S['PublicQuote'];
export type PublicQuoteOption = S['PublicQuoteOption'];
export type PublicRenewal = S['PublicRenewal'];
export type PublicSignature = S['PublicSignature'];
export type PublicDeposit = S['PublicDeposit'];
export type PublicQuoteAccept = S['PublicQuoteAccept'];
export type PublicAcceptResult = S['PublicAcceptResult'];
export type PublicRenewalResult = S['PublicRenewalResult'];
export type PublicSignatureResult = S['PublicSignatureResult'];
export type RequiredField = S['RequiredField'];

type LinkRef = { slug: string; token: string };

export function usePublicLink({ slug, token }: LinkRef) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.publicLink(slug, token),
    // 404 link_invalid is final (expired, revoked or unknown answer the same).
    retry: (count, error) => !(error instanceof ProblemError && error.status < 500) && count < 2,
    staleTime: 0,
    gcTime: 0,
    queryFn: () => unwrap(api.GET(L, { params: { path: { slug, token } } })),
  });
}

/** PDF of the link (quote, proposal or document to sign), as a blob for an <iframe>. */
export function usePublicLinkDocument({ slug, token }: LinkRef, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.publicLink(slug, token), 'document'],
    enabled,
    retry: (count, error) =>
      error instanceof ProblemError && error.type === 'not_ready' && count < 5,
    retryDelay: 2000,
    gcTime: 0,
    queryFn: async () => {
      const data = (await unwrap(
        api.GET(`${L}/document`, { params: { path: { slug, token } }, parseAs: 'blob' }),
      )) as unknown;
      return data instanceof Blob ? data : new Blob([String(data)], { type: 'application/pdf' });
    },
  });
}

function useLinkMutation<V, R>({ slug, token }: LinkRef, fn: (vars: V) => Promise<R>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSettled: () => qc.invalidateQueries({ queryKey: queryKeys.publicLink(slug, token) }),
  });
}

export function useAcceptPublicQuote(ref: LinkRef) {
  const api = useApi();
  return useLinkMutation(ref, (body: PublicQuoteAccept) =>
    unwrap(api.POST(`${L}/quote/accept`, { params: { path: ref }, body })),
  );
}

export const PUBLIC_UPLOAD_TYPES = ['application/pdf', 'image/png', 'image/jpeg'] as const;
export const PUBLIC_UPLOAD_MAX_BYTES = 10 * 1024 * 1024;

/** Uploads a customer document (raw body) from the quote acceptance page. */
export function useUploadPublicDocument(ref: LinkRef) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { file: Blob; type: string; expiresOn?: string }) =>
      unwrap(
        api.POST(`${L}/documents`, {
          params: {
            path: ref,
            query: { type: v.type, ...(v.expiresOn ? { expires_on: v.expiresOn } : {}) } as never,
          },
          body: v.file as never,
          bodySerializer: (b: unknown) => b as Blob,
          headers: { 'Content-Type': v.file.type || 'application/octet-stream' },
        }),
      ),
  });
}

export function useAcceptPublicRenewal(ref: LinkRef) {
  const api = useApi();
  return useLinkMutation(ref, (plan: S['PlanChoice'] | undefined) =>
    unwrap(api.POST(`${L}/renewal/accept`, { params: { path: ref }, body: plan ? { plan } : {} })),
  );
}

export function useDeclinePublicRenewal(ref: LinkRef) {
  const api = useApi();
  return useLinkMutation(ref, (reason: string | undefined) =>
    unwrap(
      api.POST(`${L}/renewal/decline`, {
        params: { path: ref },
        body: reason ? { reason } : {},
      }),
    ),
  );
}

export function useRequestSignatureOtp(ref: LinkRef) {
  const api = useApi();
  return useMutation({
    mutationFn: () =>
      unwrap(
        api.POST(`${L}/signature/otp`, {
          params: { path: ref },
          body: { identity_confirmed: true },
        }),
      ),
  });
}

export function useVerifySignature(ref: LinkRef) {
  const api = useApi();
  return useLinkMutation(ref, (code: string) =>
    unwrap(api.POST(`${L}/signature/verify`, { params: { path: ref }, body: { code } })),
  );
}

export function useDeclineSignature(ref: LinkRef) {
  const api = useApi();
  return useLinkMutation(ref, (reason: string | undefined) =>
    unwrap(
      api.POST(`${L}/signature/decline`, {
        params: { path: ref },
        body: reason ? { reason } : {},
      }),
    ),
  );
}

export function usePublicUnsubscribe(ref: LinkRef) {
  const api = useApi();
  return useLinkMutation(ref, () =>
    unwrap(
      api.POST(`${L}/unsubscribe`, {
        params: { path: ref },
        body: 'List-Unsubscribe=One-Click' as never,
        bodySerializer: (b: unknown) => String(b),
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      }),
    ),
  );
}
