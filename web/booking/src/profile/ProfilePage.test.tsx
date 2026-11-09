import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';

import {
  ApiProvider,
  createApiClient,
  PUBLIC_OCCUPANCY_REFETCH_MS,
  queryKeys,
  type PublicOccupancy,
  type PublicPriceMatrix,
  type PublicProfile,
  type PublicProfileMap,
} from '@porto/shared/api';
import { createFetchMock, renderWithProviders, type MockRoute } from '@porto/shared/testing';

import { ProfilePage } from '.';
import { hasHours, photoSrc, previewMarinaId } from './lib';
import { ProfileLink } from './ProfileLink';
import { SimpleMap } from './SimpleMap';

const SLUG = 'prova';
const P = `/api/public/v1/${SLUG}`;

const base: PublicProfile = {
  slug: SLUG,
  name: 'Marina Prova',
  lang: 'it',
  locales: ['it', 'en'],
  description: 'Un porto sicuro',
  latitude: '43.7101234',
  longitude: '-10.4012345',
  phone: '+39 050 000000',
  email: 'info@prova.example',
  vhf_channels: [9, 16],
  office_hours: [{ weekday: 1, from: '08:00', to: '19:00' }],
  berths_total: 120,
  transit_berths: 20,
  depth_min_cm: 250,
  depth_max_cm: 600,
  seabed: 'sand',
  shelter: ['N', 'NE'],
  services: { water: true, wifi: true, power_amps: [16, 32], fuel: { diesel: true } },
  photos: [
    { url: `${P}/profile/photos/f1`, alt: 'Ingresso' },
    { url: `${P}/profile/photos/f2`, alt: 'Pontile' },
  ],
  sections: { map: false, prices: false, occupancy: false },
  page_url: `https://porto.example/p/${SLUG}`,
  version: 3,
};

const map: PublicProfileMap = {
  min_x_cm: 0,
  min_y_cm: 0,
  max_x_cm: 10000,
  max_y_cm: 8000,
  structures: [
    {
      points: [
        [0, 1000],
        [10000, 1000],
      ],
      width_cm: 300,
    },
  ],
  berths: [
    {
      points: [
        [100, 1200],
        [500, 1200],
        [500, 2400],
        [100, 2400],
      ],
      closed: true,
      category: 'L12',
    },
  ],
  areas: [],
  land: [],
  poi: [{ kind: 'fuel', x_cm: 200, y_cm: 300 }],
  version: 4,
};

const prices: PublicPriceMatrix = {
  quoted_on: '2027-06-01',
  currency: 'EUR',
  price_includes_vat: true,
  rows: [{ key: 'L10', label: 'L10', max_loa_cm: 1000 }],
  columns: [{ key: 'ALTA', label: 'Alta', arrival: '2027-06-01', departure: '2027-06-02' }],
  cells: [{ row: 0, col: 0, total_cents: 3000 }],
};

const occupancy: PublicOccupancy = {
  as_of: '2027-06-01T07:00:00Z',
  night: '2027-06-01',
  categories: [
    {
      code: 'L12',
      name: { it: 'Fino a 12 m', en: 'Up to 12 m' },
      free_berths: 19,
      total_berths: 20,
    },
  ],
};

function render(profile: PublicProfile, extra: MockRoute[] = []) {
  const mock = createFetchMock([
    { path: `${P}/profile`, body: profile },
    { path: `${P}/profile/map`, body: map },
    { path: `${P}/profile/prices`, body: prices },
    { path: `${P}/occupancy`, body: occupancy },
    ...extra,
  ]);
  const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
  const r = renderWithProviders(
    <ApiProvider client={api}>
      <ProfilePage slug={SLUG} />
    </ApiProvider>,
  );
  return { ...r, calls: mock.calls };
}

afterEach(() => window.history.replaceState(null, '', '/'));

describe('public profile page', () => {
  it('shows the sections with data and hides the optional ones when their flags are off', async () => {
    const { calls } = render(base);
    expect(await screen.findByTestId('profile-name')).toHaveTextContent('Marina Prova');
    expect(screen.getByRole('link', { name: /\+39 050 000000/ })).toHaveAttribute(
      'href',
      'tel:+39050000000',
    );
    expect(screen.getByRole('link', { name: /info@prova.example/ })).toHaveAttribute(
      'href',
      'mailto:info@prova.example',
    );
    expect(screen.getByTestId('profile-section-hours')).toHaveTextContent('Lunedì');
    expect(screen.getByTestId('profile-section-features')).toHaveTextContent('2,50 m – 6,00 m');
    expect(screen.getByTestId('shelter-rose')).toBeInTheDocument();
    expect(within(screen.getByTestId('profile-services')).getByText('Wi-Fi')).toBeInTheDocument();
    expect(screen.queryByTestId('profile-section-map')).toBeNull();
    expect(screen.queryByTestId('profile-section-prices')).toBeNull();
    expect(screen.queryByTestId('profile-section-occupancy')).toBeNull();
    expect(screen.queryByTestId('profile-book')).toBeNull(); // booking disabled: no booking_url
    expect(
      calls.some((c) => c.url.pathname.endsWith('/occupancy') || c.url.pathname.endsWith('/map')),
    ).toBe(false);
  });

  it('shows map, prices, occupancy and the booking button when published', async () => {
    const r = render({
      ...base,
      booking_url: `/b/${SLUG}`,
      sections: { map: true, prices: true, occupancy: true },
    });
    expect(await screen.findByTestId('simple-map')).toBeInTheDocument();
    expect(await screen.findByTestId('profile-prices')).toHaveTextContent('30,00');
    expect(await screen.findByTestId('profile-occupancy')).toHaveTextContent('19 liberi su 20');
    expect(screen.getByTestId('profile-book')).toHaveAttribute('href', `/b/${SLUG}`);
    const q = r.queryClient.getQueryCache().find({ queryKey: queryKeys.publicOccupancy(SLUG) });
    expect((q?.options as { refetchInterval?: number }).refetchInterval).toBe(
      PUBLIC_OCCUPANCY_REFETCH_MS,
    );
    expect(PUBLIC_OCCUPANCY_REFETCH_MS).toBe(60_000);
  });

  it('cycles the photos and switches language', async () => {
    const user = userEvent.setup();
    const { calls } = render(base);
    const photos = await screen.findByTestId('profile-photos');
    expect(within(photos).getByRole('img')).toHaveAttribute('alt', 'Ingresso');
    await user.click(screen.getByRole('button', { name: 'Foto successiva' }));
    expect(within(photos).getByRole('img')).toHaveAttribute('alt', 'Pontile');
    await user.click(within(screen.getByTestId('profile-lang')).getByText('English'));
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('lang') === 'en')).toBe(true),
    );
    expect(window.location.search).toBe('?lang=en');
  });

  it('shows not found for an unpublished or unknown slug', async () => {
    const mock = createFetchMock([
      { path: `${P}/profile`, status: 404, body: { type: 'not_found', status: 404 } },
    ]);
    const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
    renderWithProviders(
      <ApiProvider client={api}>
        <ProfilePage slug={SLUG} />
      </ApiProvider>,
    );
    expect(await screen.findByText('Questa scheda non è disponibile.')).toBeInTheDocument();
  });

  it('loads the draft through the panel in preview mode', async () => {
    window.history.replaceState(null, '', `/p/${SLUG}?preview=1&marina=m-1`);
    const { calls } = render(base, [
      {
        path: '/api/admin/v1/marinas/m-1/profile/preview',
        body: { profile: { ...base, name: 'Bozza' }, json_ld: {} },
      },
    ]);
    expect(await screen.findByTestId('profile-name')).toHaveTextContent('Bozza');
    expect(screen.getByTestId('profile-preview-banner')).toBeInTheDocument();
    expect(within(screen.getByTestId('profile-photos')).getByRole('img')).toHaveAttribute(
      'src',
      '/api/admin/v1/marinas/m-1/profile/photos/f1',
    );
    expect(calls.some((c) => c.url.pathname === `${P}/profile`)).toBe(false);
  });
});

describe('profile helpers and simplified map', () => {
  it('reads the preview query and maps photo URLs', () => {
    expect(previewMarinaId('?preview=1&marina=abc')).toBe('abc');
    expect(previewMarinaId('?marina=abc')).toBeNull();
    expect(photoSrc(null)('/x/y')).toBe('/x/y');
    expect(photoSrc('m')(`${P}/profile/photos/f9`)).toBe(
      '/api/admin/v1/marinas/m/profile/photos/f9',
    );
    expect(hasHours({ ...base, office_hours: [], dock_hours: [], hours_note: undefined })).toBe(
      false,
    );
  });

  it('renders shapes in map centimetres without codes', () => {
    const r = renderWithProviders(<SimpleMap map={map} />);
    const svg = screen.getByTestId('simple-map');
    expect(svg.getAttribute('viewBox')).toBe('0 0 10000 8000');
    expect(svg.querySelectorAll('[data-layer="berths"] polygon')).toHaveLength(1);
    expect(svg.querySelectorAll('[data-layer="structures"] polyline')).toHaveLength(1);
    expect(svg.querySelector('[data-kind="fuel"]')).not.toBeNull();
    expect(r.container.innerHTML).not.toMatch(/A-\d|data-code/);
  });
});

describe('ProfileLink', () => {
  it('links /p/{slug} only when the profile is published', async () => {
    const card = {
      slug: SLUG,
      name: 'Marina Prova',
      default_locale: 'it',
      locales: ['it'],
      booking_enabled: true,
    };
    const mock = createFetchMock([{ path: P, body: { ...card, profile_published: true } }]);
    const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
    renderWithProviders(
      <ApiProvider client={api}>
        <ProfileLink slug={SLUG} />
      </ApiProvider>,
    );
    expect(await screen.findByTestId('profile-link')).toHaveAttribute('href', `/p/${SLUG}`);
    expect(screen.getByTestId('profile-link')).toHaveTextContent('Scheda del porto');

    const off = createFetchMock([{ path: P, body: card }]);
    const api2 = createApiClient({ fetch: off.fetch, baseUrl: 'http://localhost' });
    const r = renderWithProviders(
      <ApiProvider client={api2}>
        <ProfileLink slug={SLUG} />
      </ApiProvider>,
    );
    await waitFor(() => expect(off.calls.length).toBe(1));
    expect(r.container.querySelector('[data-testid="profile-link"]')).toBeNull();
  });
});
