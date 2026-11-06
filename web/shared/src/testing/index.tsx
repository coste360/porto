import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, type RenderOptions, type RenderResult } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { I18nextProvider } from 'react-i18next';

import { initI18n } from '../i18n/init';
import type { Locale } from '../i18n/resources';
import { ThemeProvider } from '../theme/ThemeProvider';

export function createTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
}

export type ProvidersOptions = { queryClient?: QueryClient; locale?: Locale };

export function TestProviders({
  children,
  queryClient,
  locale = 'it',
}: ProvidersOptions & { children: ReactNode }) {
  const client = queryClient ?? createTestQueryClient();
  const i18n = initI18n({ lng: locale });
  return (
    <I18nextProvider i18n={i18n}>
      <ThemeProvider defaultTheme="light">
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </ThemeProvider>
    </I18nextProvider>
  );
}

/** Renders with QueryClient (no retries), i18n (Italian by default) and the theme. */
export function renderWithProviders(
  ui: ReactElement,
  options: ProvidersOptions & Omit<RenderOptions, 'wrapper'> = {},
): RenderResult & { queryClient: QueryClient } {
  const { queryClient = createTestQueryClient(), locale, ...rest } = options;
  const result = render(ui, {
    wrapper: ({ children }) => (
      <TestProviders queryClient={queryClient} {...(locale ? { locale } : {})}>
        {children}
      </TestProviders>
    ),
    ...rest,
  });
  return { ...result, queryClient };
}

export type MockRoute = {
  method?: string;
  /** Path (no query string) or a predicate on the URL. */
  path: string | ((url: URL) => boolean);
  status?: number;
  body?: unknown;
  headers?: Record<string, string>;
};

export type FetchCall = { method: string; url: URL; headers: Headers; body: unknown };

/**
 * Minimal fetch mock (no MSW): routes are matched in order, unmatched requests get 404
 * problem+json. Records every call for assertions.
 */
export function createFetchMock(routes: MockRoute[]) {
  const calls: FetchCall[] = [];
  const fetchMock = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const request = input instanceof Request ? input : new Request(String(input), init);
    const url = new URL(request.url, 'http://localhost');
    const method = request.method.toUpperCase();
    let body: unknown = undefined;
    const text = await request.clone().text();
    if (text) {
      try {
        body = JSON.parse(text);
      } catch {
        body = text;
      }
    }
    calls.push({ method, url, headers: request.headers, body });
    const route = routes.find(
      (r) =>
        (r.method ?? 'GET').toUpperCase() === method &&
        (typeof r.path === 'string' ? r.path === url.pathname : r.path(url)),
    );
    if (!route) {
      return new Response(JSON.stringify({ type: 'not_found', title: 'Not Found', status: 404 }), {
        status: 404,
        headers: { 'Content-Type': 'application/problem+json' },
      });
    }
    const status = route.status ?? 200;
    const isProblem = status >= 400;
    const hasBody = route.body !== undefined && status !== 204;
    return new Response(hasBody ? JSON.stringify(route.body) : null, {
      status,
      headers: {
        'Content-Type': isProblem ? 'application/problem+json' : 'application/json',
        ...route.headers,
      },
    });
  };
  return { fetch: fetchMock as typeof fetch, calls };
}

export function problem(type: string, status: number, extra: Record<string, unknown> = {}) {
  return { type, title: type, status, ...extra };
}
