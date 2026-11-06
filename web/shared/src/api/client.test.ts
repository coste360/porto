import { describe, expect, it, vi } from 'vitest';

import { createFetchMock, problem } from '../testing';
import { CSRF_HEADER, createApiClient, ifMatch, unwrap, uploadBinary } from './client';
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

describe('uploadBinary', () => {
  it('sends the raw body with content type and CSRF header, and parses the JSON answer', async () => {
    const calls: Request[] = [];
    const fake: typeof fetch = async (input, init) => {
      calls.push(new Request(input as string, init));
      return new Response(JSON.stringify({ id: 'd1' }), { status: 200, headers: { ETag: '"2"' } });
    };
    const { data, response } = await uploadBinary<{ id: string }>(
      'http://local/api/admin/v1/marinas/m/documents/d/file',
      new Uint8Array([37, 80, 68, 70]),
      {
        method: 'PUT',
        contentType: 'application/pdf',
        headers: { 'X-File-Name': 'a.pdf' },
        fetch: fake,
        readCsrf: () => 'tok',
      },
    );
    expect(data.id).toBe('d1');
    expect(response.headers.get('ETag')).toBe('"2"');
    expect(calls[0]?.method).toBe('PUT');
    expect(calls[0]?.headers.get('Content-Type')).toBe('application/pdf');
    expect(calls[0]?.headers.get('X-CSRF-Token')).toBe('tok');
    expect(calls[0]?.headers.get('X-File-Name')).toBe('a.pdf');
  });

  it('throws a ProblemError on errors', async () => {
    const fake: typeof fetch = async () =>
      new Response(JSON.stringify({ type: 'file_too_large', title: 'x', status: 413 }), {
        status: 413,
        headers: { 'Content-Type': 'application/problem+json' },
      });
    await expect(
      uploadBinary('http://local/x', new Uint8Array([1]), {
        method: 'POST',
        contentType: 'image/png',
        fetch: fake,
      }),
    ).rejects.toMatchObject({ type: 'file_too_large', status: 413 });
  });
});
