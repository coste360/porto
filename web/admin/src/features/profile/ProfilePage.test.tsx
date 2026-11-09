import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { MarinaProfile } from '@porto/shared/api';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import '../pricing/test/dom';
import {
  canPublish,
  degreesText,
  parseDegrees,
  previewUrl,
  profileSchema,
  toInput,
  toValues,
} from './lib/profileForm';

const M = `/api/admin/v1/marinas/${MARINA_ID}`;

const draft: MarinaProfile = {
  published: false,
  version: 0,
  berths_total: 120,
  transit_berths: 20,
  prices_available: false,
  public_url: 'https://porto.example/p/prova',
  missing: ['position', 'contact'],
  locales: ['it'],
  services: {},
  photos: [],
  access_images: [],
  office_hours: [],
  dock_hours: [],
  vhf_channels: [],
  shelter: [],
  occupancy_categories: [],
  poi: [],
  show_map: true,
};

const complete: MarinaProfile = {
  ...draft,
  version: 3,
  missing: [],
  entrance_lat_e7: 437101234,
  entrance_lon_e7: -104012345,
  phone: '+39 050 000000',
  depth_min_cm: 250,
  depth_max_cm: 600,
  description: { it: 'Un porto', en: 'A marina' },
};

const admin = makeMe({}, ['marina.read', 'marina.configure', 'map.view', 'pricing.read']);

function routes(p: MarinaProfile) {
  return [
    meRoute(admin),
    { path: `${M}/profile`, body: p },
    { method: 'PUT', path: `${M}/profile`, body: { ...p, version: p.version + 1 } },
    {
      method: 'POST',
      path: `${M}/profile/publish`,
      body: { ...p, published: true, version: p.version + 1 },
    },
    { path: `${M}/maps`, body: { items: [] } },
    { path: `${M}/berth-categories`, body: { items: [] } },
  ];
}

describe('profile form helpers', () => {
  it('parses decimal degrees to exact e7 integers', () => {
    expect(parseDegrees('43.7101234', 'lat')).toBe(437101234);
    expect(parseDegrees('43,7101234', 'lat')).toBe(437101234);
    expect(parseDegrees('-10.4012345', 'lon')).toBe(-104012345);
    expect(parseDegrees('0.00000005', 'lat')).toBe(0); // half-even
    expect(parseDegrees('0.00000015', 'lat')).toBe(2);
    expect(parseDegrees('91', 'lat')).toBeNull();
    expect(parseDegrees('179.9999999', 'lon')).toBe(1799999999);
    expect(parseDegrees('1e3', 'lat')).toBeNull();
    expect(parseDegrees('abc', 'lon')).toBeNull();
    expect(degreesText(-5000000)).toBe('-0.5000000');
    expect(degreesText(null)).toBe('');
  });

  it('round-trips a profile with depths in cm and the position in e7', () => {
    const v = toValues(complete);
    expect(v.lat).toBe('43.7101234');
    expect(v.lon).toBe('-10.4012345');
    expect(profileSchema.safeParse(v).success).toBe(true);
    const input = toInput(v);
    expect(input.entrance_lat_e7).toBe(437101234);
    expect(input.entrance_lon_e7).toBe(-104012345);
    expect(input.depth_min_cm).toBe(250);
    expect(input.depth_max_cm).toBe(600);
    expect(input.description).toEqual({ it: 'Un porto', en: 'A marina' });
    expect(input.locales).toEqual(['it']);
  });

  it('refuses half a position, min depth above max and occupancy without categories', () => {
    const v = toValues(complete);
    v.lon = '';
    expect(profileSchema.safeParse(v).success).toBe(false);
    const w = toValues(complete);
    w.depth_min_cm = 700;
    expect(profileSchema.safeParse(w).success).toBe(false);
    const x = toValues(complete);
    x.show_occupancy = true;
    expect(profileSchema.safeParse(x).success).toBe(false);
    const y = toValues(complete);
    y.website = 'http://insecure.example';
    expect(profileSchema.safeParse(y).success).toBe(false);
  });

  it('gates publication on the missing items and builds the preview URL', () => {
    expect(canPublish(draft)).toBe(false);
    expect(canPublish(complete)).toBe(true);
    expect(previewUrl('https://porto.example/p/prova', 'm-1')).toBe(
      '/p/prova?preview=1&marina=m-1',
    );
  });
});

describe('ProfilePage', () => {
  it('lists what is missing and keeps Publish disabled', async () => {
    renderApp(`/m/${MARINA_ID}/profile`, routes(draft));
    expect(await screen.findByTestId('profile-missing')).toHaveTextContent(
      "la posizione dell'imboccatura",
    );
    expect(screen.getByTestId('profile-publish')).toBeDisabled();
    expect(screen.getByTestId('profile-status')).toHaveTextContent('Non pubblicata');
    expect(screen.getByTestId('profile-url')).toHaveTextContent('https://porto.example/p/prova');
  });

  it('saves with If-Match and publishes a complete profile', async () => {
    const user = userEvent.setup();
    const { calls } = renderApp(`/m/${MARINA_ID}/profile`, routes(complete));
    const publish = await screen.findByTestId('profile-publish');
    expect(publish).toBeEnabled();
    await user.click(screen.getByRole('tab', { name: 'Contatti' }));
    await user.type(await screen.findByLabelText('Email'), 'info@porto.example');
    await user.click(screen.getByTestId('profile-save'));
    await waitFor(() => {
      const put = calls.find((c) => c.method === 'PUT');
      expect(put?.headers.get('If-Match')).toBe('"3"');
      const body = put?.body as { email: string; entrance_lat_e7: number };
      expect(body.email).toBe('info@porto.example');
      expect(body.entrance_lat_e7).toBe(437101234);
    });
    await user.click(publish);
    await waitFor(() =>
      expect(
        calls.some((c) => c.method === 'POST' && c.url.pathname.endsWith('/profile/publish')),
      ).toBe(true),
    );
  });

  it('edits a text per language and shows the prices hint', async () => {
    const user = userEvent.setup();
    renderApp(`/m/${MARINA_ID}/profile`, routes(complete));
    expect(await screen.findByLabelText('Descrizione (Italiano)')).toHaveValue('Un porto');
    await user.click(screen.getAllByRole('tab', { name: 'Inglese' })[0] as HTMLElement);
    expect(await screen.findByLabelText('Descrizione (Inglese)')).toHaveValue('A marina');
    await user.click(screen.getByRole('tab', { name: 'Sezioni' }));
    expect(await screen.findByTestId('profile-prices-hint')).toBeInTheDocument();
  });
});
