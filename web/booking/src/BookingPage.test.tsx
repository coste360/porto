import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { ApiProvider, createApiClient } from '@porto/shared/api';
import { createFetchMock, problem, renderWithProviders } from '@porto/shared/testing';

import { BookingPage } from './BookingPage';

function renderPage(slug: string | null, fetchMock: ReturnType<typeof createFetchMock>) {
  const api = createApiClient({ fetch: fetchMock.fetch, baseUrl: 'http://localhost' });
  return renderWithProviders(
    <ApiProvider client={api}>
      <BookingPage slug={slug} />
    </ApiProvider>,
  );
}

describe('BookingPage', () => {
  it('shows the marina name and the coming-soon notice', async () => {
    const mock = createFetchMock([
      {
        path: '/api/public/v1/prova',
        body: {
          slug: 'prova',
          name: 'Marina Prova',
          default_locale: 'it',
          locales: ['it'],
          booking_enabled: false,
        },
      },
    ]);
    renderPage('prova', mock);
    expect(await screen.findByRole('heading', { name: 'Marina Prova' })).toBeInTheDocument();
    expect(screen.getByText('La prenotazione online arriverà presto')).toBeInTheDocument();
    expect(screen.getByText('powered by Porto')).toBeInTheDocument();
  });

  it('shows the not-found page for an unknown marina', async () => {
    const mock = createFetchMock([
      { path: '/api/public/v1/nope', status: 404, body: problem('not_found', 404) },
    ]);
    renderPage('nope', mock);
    expect(await screen.findByText('Porto non trovato')).toBeInTheDocument();
  });

  it('shows the not-found page without a slug', () => {
    renderPage(null, createFetchMock([]));
    expect(screen.getByText('Porto non trovato')).toBeInTheDocument();
  });
});
