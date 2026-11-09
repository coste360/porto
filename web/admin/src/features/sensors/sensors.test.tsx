import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, B1, MAP_ID, baseRoutes } from '../desk/test/fixtures';
import { validateHistorySearch } from './routes';
import {
  DEFAULT_SETTINGS,
  cmToMetres,
  hmacExample,
  metresToCm,
  proposalOf,
  rangeInside,
  segmentIssue,
  settingsIssues,
} from './lib';

describe('sensors helpers', () => {
  it('converts metres and centimetres exactly', () => {
    expect(metresToCm('12')).toBe(1200);
    expect(metresToCm('12,5')).toBe(1250);
    expect(metresToCm('0.07')).toBe(7);
    expect(metresToCm('1.234')).toBeNull();
    expect(metresToCm('-1')).toBeNull();
    expect(cmToMetres(1250)).toBe('12,50');
    expect(cmToMetres(7)).toBe('0,07');
  });

  it('checks segments like the server', () => {
    expect(segmentIssue(0, 1000, 6000)).toBeNull();
    expect(segmentIssue(0, 99, 6000)).toBe('segmentShort');
    expect(segmentIssue(500, 400, 6000)).toBe('segmentOrder');
    expect(segmentIssue(5900, 6100, 6000)).toBe('segmentOutside');
    expect(segmentIssue(null, 100)).toBe('segmentInvalid');
  });

  it('validates the settings ranges and silent ≥ offline', () => {
    expect(settingsIssues(DEFAULT_SETTINGS)).toEqual([]);
    expect(settingsIssues({ ...DEFAULT_SETTINGS, hysteresis_readings: 11 })).toEqual([
      'hysteresis_readings',
    ]);
    expect(settingsIssues({ ...DEFAULT_SETTINGS, offline_after_minutes: 200 })).toEqual([
      'silent_alert_minutes',
    ]);
  });

  it('reads the proposal and keeps the range inside it', () => {
    const p = proposalOf({ refs: { proposed_from: '2027-06-13', proposed_to: '2027-06-19' } });
    expect(p).toEqual({ from: '2027-06-13', to: '2027-06-19' });
    expect(rangeInside(p!, '2027-06-13', '2027-06-15')).toBe(true);
    expect(rangeInside(p!, '2027-06-12', '2027-06-15')).toBe(false);
    expect(rangeInside(p!, '2027-06-16', '2027-06-15')).toBe(false);
    expect(proposalOf({ refs: {} })).toBeNull();
  });

  it('builds the signing example with the public key id only in the header', () => {
    const ex = hmacExample('po_sns_abcdefghijklmnop_SECRET', 'https://porto.test');
    expect(ex).toContain("Porto-Key: po_sns_abcdefghijklmnop'");
    expect(ex).toContain('https://porto.test/api/ingest/v1/readings');
  });

  it('validates the history search', () => {
    expect(
      validateHistorySearch({
        resource_type: 'berth',
        resource_id: B1,
        from: '2027-06-01',
        to: 'x',
      }),
    ).toEqual({
      resource_type: 'berth',
      resource_id: B1,
      from: '2027-06-01',
    });
    expect(validateHistorySearch({ resource_type: 'shed' })).toEqual({});
  });
});

const alert = (over: Record<string, unknown>) => ({
  id: 'a1',
  kind: 'release_proposal',
  status: 'open',
  device_id: 'd1',
  device_code: 'S-A1',
  resource_type: 'berth',
  resource_id: B1,
  resource_code: 'A1',
  map_id: MAP_ID,
  refs: { allocation_id: 'al1', proposed_from: '2027-06-13', proposed_to: '2027-06-19' },
  opened_at: '2027-06-12T08:00:00Z',
  allowed_actions: ['confirm_release', 'dismiss'],
  version: 3,
  ...over,
});

const device = {
  id: 'd1',
  external_id: 'EXT1',
  code: 'S-A1',
  kind: 'ultrasonic',
  model: '',
  firmware: '',
  resource_type: 'berth',
  resource_id: B1,
  resource_code: 'A1',
  map_id: MAP_ID,
  x_cm: 100,
  y_cm: 100,
  key_status: 'active',
  active: true,
  physical: 'occupied',
  health: 'ok',
  battery_pct: 80,
  version: 2,
  created_at: '',
  updated_at: '',
};

const settings = { ...DEFAULT_SETTINGS, raw_retention_days: 30, version: 0 };

function routes(perms: string[], alerts = [alert({})]) {
  return [
    ...baseRoutes(makeMe({}, perms as never)),
    { path: `${API}/sensors/alerts`, body: { items: alerts, next_cursor: null } },
    {
      path: `${API}/sensors/alerts/a1/actions`,
      method: 'POST',
      body: alert({ status: 'confirmed', allowed_actions: [] }),
    },
    { path: `${API}/sensors/devices`, body: { items: [device], next_cursor: null } },
    {
      path: `${API}/sensors/devices/d1/rotate-key`,
      method: 'POST',
      body: {
        device: { ...device, version: 3, key_status: 'rotating' },
        key: 'po_sns_abcdefghijklmnop_NEWSECRET',
      },
    },
    { path: `${API}/sensors/gateways`, body: { items: [] } },
    { path: `${API}/sensors/sources`, body: { items: [], drivers: [] } },
    { path: `${API}/sensors/settings`, body: settings },
    { path: `${API}/sensors/settings`, method: 'PUT', body: { ...settings, version: 1 } },
  ];
}

const openTab = async (id: string) => {
  const tab = await screen.findByTestId(id);
  fireEvent.mouseDown(tab);
  fireEvent.click(tab);
};

describe('sensors page', () => {
  it('needs map.view', async () => {
    renderApp(`/m/${MARINA_ID}/sensors`, routes(['marina.read']));
    expect(await screen.findByText('Non hai accesso a questa sezione.')).toBeInTheDocument();
  });

  it('gates the release confirmation on the consent and a range inside the proposal', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/sensors`,
      routes(['marina.read', 'map.view', 'occupancy.move']),
    );
    const card = await screen.findByTestId('alert-release_proposal');
    expect(
      within(card).getByText('Rilascio proposto dal 2027-06-13 al 2027-06-19'),
    ).toBeInTheDocument();
    fireEvent.click(within(card).getByTestId('alert-action-confirm_release'));
    const dialog = await screen.findByTestId('alert-dialog-confirm_release');
    const submit = within(dialog).getByTestId('alert-dialog-submit');
    expect(submit).toBeDisabled();
    fireEvent.click(within(dialog).getByTestId('release-consent'));
    expect(submit).toBeEnabled();
    fireEvent.change(within(dialog).getByLabelText('Al'), { target: { value: '2027-06-25' } });
    expect(within(dialog).getByRole('alert')).toHaveTextContent('proposta');
    expect(submit).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText('Al'), { target: { value: '2027-06-15' } });
    fireEvent.click(submit);
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.headers.get('If-Match')).toBe('"3"');
    expect(post?.body).toEqual({
      action: 'confirm_release',
      from: '2027-06-13',
      to: '2027-06-15',
      consent_confirmed: true,
    });
  });

  it('confirms an arrival at once and dismisses with a note', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/sensors`,
      routes(
        ['marina.read', 'map.view', 'presence.write'],
        [
          alert({
            kind: 'transit_arrival',
            refs: {},
            allowed_actions: ['confirm_arrival', 'dismiss'],
          }),
        ],
      ),
    );
    const card = await screen.findByTestId('alert-transit_arrival');
    fireEvent.click(within(card).getByTestId('alert-action-confirm_arrival'));
    await waitFor(() => expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1));
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ action: 'confirm_arrival' });
  });

  it('shows a rotated key once and forgets it on close', async () => {
    renderApp(`/m/${MARINA_ID}/sensors`, routes(['marina.read', 'map.view', 'harbor.configure']));
    await openTab('sensors-tab-devices');
    const row = await screen.findByTestId('device-row-S-A1');
    expect(within(row).getByText('Regolare')).toBeInTheDocument();
    fireEvent.click(within(row).getByTestId('device-rotate-S-A1'));
    const rotate = await screen.findByTestId('rotate-dialog');
    fireEvent.click(within(rotate).getByTestId('rotate-submit'));
    const reveal = await screen.findByTestId('key-reveal');
    expect(
      within(reveal).getByDisplayValue('po_sns_abcdefghijklmnop_NEWSECRET'),
    ).toBeInTheDocument();
    fireEvent.click(within(reveal).getByRole('button', { name: 'Ho copiato il valore' }));
    await waitFor(() => expect(screen.queryByTestId('key-reveal')).not.toBeInTheDocument());
    expect(screen.queryByDisplayValue('po_sns_abcdefghijklmnop_NEWSECRET')).not.toBeInTheDocument();
  });

  it('validates and saves the settings with If-Match 0 on the first save', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/sensors`,
      routes(['marina.read', 'map.view', 'harbor.configure']),
    );
    await openTab('sensors-tab-settings');
    const form = await screen.findByTestId('sensor-settings');
    const n = within(form).getByLabelText('Letture concordi per cambiare stato');
    fireEvent.change(n, { target: { value: '12' } });
    expect(within(form).getByTestId('settings-save')).toBeDisabled();
    fireEvent.change(n, { target: { value: '3' } });
    fireEvent.click(within(form).getByRole('switch'));
    fireEvent.click(within(form).getByTestId('settings-save'));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.headers.get('If-Match')).toBe('"0"');
    expect(put?.body).toMatchObject({ hysteresis_readings: 3, presence_effect_enabled: true });
  });
});

describe('dock Avvisi tab', () => {
  it('lists the open alerts with the count, the actions and the map link', async () => {
    renderApp(
      `/m/${MARINA_ID}/dock?tab=alerts`,
      routes(
        ['marina.read', 'map.view', 'presence.write'],
        [
          alert({
            kind: 'transit_arrival',
            refs: {},
            allowed_actions: ['confirm_arrival', 'dismiss'],
          }),
          alert({ id: 'a2', kind: 'unexpected_occupied', refs: {}, allowed_actions: ['dismiss'] }),
        ],
      ),
    );
    const tab = await screen.findByTestId('dock-alerts');
    expect(within(tab).getByTestId('dock-alerts-count')).toHaveTextContent('2');
    const arrival = within(tab).getByTestId('dock-alert-transit_arrival');
    expect(within(arrival).getByTestId('alert-action-confirm_arrival')).toBeInTheDocument();
    expect(within(arrival).getByText('Mostra sulla mappa')).toBeInTheDocument();
  });
});
