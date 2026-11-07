// Hooks of the public pay page of the booking SPA (m5-plan §5.5, §9.1): the page of a payment request, the start of
// an online checkout and the status polled by the return page. The token is the only credential: keys live under
// queryKeys.publicPay(slug, token), never under a marina, and nothing is cached across tokens.
import { useMutation, useQuery } from '@tanstack/react-query';

import { unwrap } from '../client';
import { useApi } from '../context';
import { ProblemError } from '../problem';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

const P = '/api/public/v1/{slug}/pay/{payToken}' as const;

export type PayPage = S['PayPage'];
export type PayCharge = S['PayCharge'];
export type PayBank = S['PayBank'];
export type PayCheckoutInput = S['PayCheckoutInput'];
export type PayCheckoutResult = S['PayCheckoutResult'];
export type PayStatus = S['PayStatus'];

type PayRef = { slug: string; token: string };

/** A 4xx (unknown, expired or revoked link) is final: no retries. */
function retryServerErrors(count: number, error: unknown) {
  return !(error instanceof ProblemError && error.status < 500) && count < 2;
}

export function usePayPage({ slug, token }: PayRef) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.publicPay(slug, token),
    retry: retryServerErrors,
    staleTime: 0,
    gcTime: 0,
    queryFn: () => unwrap(api.GET(P, { params: { path: { slug, payToken: token } } })),
  });
}

/** Starts the provider checkout; the caller redirects the browser to `redirect_url`. */
export function useStartPayCheckout({ slug, token }: PayRef) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: PayCheckoutInput) =>
      unwrap(api.POST(`${P}/checkout`, { params: { path: { slug, payToken: token } }, body })),
  });
}

/** Status of the request, polled every few seconds while an online payment is being verified. */
export function usePayStatus({ slug, token }: PayRef, pollMs = 3000) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.publicPay(slug, token), 'status'],
    retry: retryServerErrors,
    gcTime: 0,
    refetchInterval: (q) => {
      const d = q.state.data;
      if (!d) return pollMs;
      return d.status === 'open' ? pollMs : false;
    },
    queryFn: () => unwrap(api.GET(`${P}/status`, { params: { path: { slug, payToken: token } } })),
  });
}
