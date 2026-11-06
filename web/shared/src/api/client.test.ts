import { describe, expect, it, vi } from 'vitest';

import { createFetchMock, problem } from '../testing';
import { CSRF_HEADER, createApiClient, ifMatch, unwrap } from './client';
import { ProblemError } from './problem';

describe('api client', () => {
  it('adds the CSRF header on unsafe methods only', async () => {
    const mock = createFetchMock([
      { method: 'POST', path: '/api/admin/v1/auth/logout', status: 204 },
      { path: '/api/admin/v1/organizations', body: { items: [] } },
    ]);
    const api = createApiClient({
      fetch: mock.fetch,
      baseUrl: 'http://localhost',
      readCsrf: () => 'csrf-123',
    });
    await api.POST('/api/admin/v1/auth/logout');
    await api.GET('/api/admin/v1/organizations');
    expect(mock.calls[0]?.headers.get(CSRF_HEADER)).toBe('csrf-123');
    expect(mock.calls[1]?.headers.get(CSRF_HEADER)).toBeNull();
  });

  it('reads the CSRF token from the porto_csrf cookie by default', async () => {
    document.cookie = 'porto_csrf=from-cookie; path=/';
    const mock = createFetchMock([
      { method: 'POST', path: '/api/admin/v1/auth/logout', status: 204 },
    ]);
    const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
    await api.POST('/api/admin/v1/auth/logout');
    expect(mock.calls[0]?.headers.get(CSRF_HEADER)).toBe('from-cookie');
    document.cookie = 'porto_csrf=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/';
  });

  it('calls onUnauthorized on a 401, except for the anonymous endpoints', async () => {
    const onUnauthorized = vi.fn();
    const mock = createFetchMock([
      { path: '/api/admin/v1/organizations', status: 401, body: problem('unauthorized', 401) },
      { path: '/api/admin/v1/auth/me', status: 401, body: problem('unauthorized', 401) },
      {
        method: 'POST',
        path: '/api/admin/v1/auth/login',
        status: 401,
        body: problem('invalid_credentials', 401),
      },
    ]);
    const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost', onUnauthorized });
    await api.GET('/api/admin/v1/auth/me');
    await api.POST('/api/admin/v1/auth/login', { body: { email: 'a@b.it', password: 'x' } });
    expect(onUnauthorized).not.toHaveBeenCalled();
    await api.GET('/api/admin/v1/organizations');
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('unwrap returns data or throws a ProblemError', async () => {
    const mock = createFetchMock([
      { path: '/api/admin/v1/organizations', body: { items: [] } },
      { path: '/api/admin/v1/event-types', status: 403, body: problem('forbidden', 403) },
    ]);
    const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
    await expect(unwrap(api.GET('/api/admin/v1/organizations'))).resolves.toEqual({ items: [] });
    const error = await unwrap(api.GET('/api/admin/v1/event-types')).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ProblemError);
    expect((error as ProblemError).type).toBe('forbidden');
  });

  it('formats If-Match as a quoted version', () => {
    expect(ifMatch(7)).toBe('"7"');
  });
});
