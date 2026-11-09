import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, baseRoutes } from '../desk/test/fixtures';
import {
  defaultHours,
  formulaFields,
  hoursValid,
  licenceRequired,
  parseSlotLabel,
  probeRange,
  slotLabel,
  stripCells,
  toMilli,
  windowIssues,
} from './lib';

const morning = { code: 'morning', from_slot: 16, to_slot: 26 };
const afternoon = { code: 'afternoon', from_slot: 28, to_slot: 38 };

describe('rental fleet helpers', () => {
  it('labels and parses slots', () => {
    expect(slotLabel(0)).toBe('00:00');
    expect(slotLabel(21)).toBe('10:30');
    expect(slotLabel(48)).toBe('24:00');
    expect(parseSlotLabel('09:45')).toBe(19);
    expect(parseSlotLabel('24:00')).toBe(48);
    expect(parseSlotLabel('25:00')).toBeNull();
    expect(parseSlotLabel('nope')).toBeNull();
  });

  it('validates the open window, the bands and the buffer like the server', () => {
    expect(windowIssues(16, 38, [morning, afternoon], 1)).toEqual([]);
    expect(windowIssues(16, 38, [morning, afternoon], 3)).toEqual(['band_gap_lt_buffer']);
    expect(windowIssues(4, 38, [morning], 1)).toEqual(['window_dst']);
    expect(windowIssues(38, 16, [morning], 1)).toContain('window_invalid');
    expect(windowIssues(16, 38, [], 1)).toEqual(['bands_count']);
    expect(windowIssues(16, 30, [morning, afternoon], 1)).toEqual(['band_outside']);
    expect(windowIssues(16, 38, [morning, { ...afternoon, code: 'morning' }], 1)).toEqual([
      'band_code',
    ]);
    expect(windowIssues(0, 48, [{ code: 'night', from_slot: 2, to_slot: 8 }], 0)).toEqual([
      'window_dst',
      'band_dst',
    ]);
    expect(windowIssues(16, 38, [morning], 9)).toEqual(['buffer_range']);
  });

  it('draws the strip cells', () => {
    const cells = stripCells(16, 38, [morning, afternoon]);
    expect(cells).toHaveLength(48);
    expect(cells[4]?.kind).toBe('dst');
    expect(cells[15]?.kind).toBe('closed');
    expect(cells[16]).toEqual({ slot: 16, kind: 'band', band: 0 });
    expect(cells[26]?.kind).toBe('open');
    expect(cells[30]).toEqual({ slot: 30, kind: 'band', band: 1 });
    expect(cells[38]?.kind).toBe('closed');
  });

  it('checks the weekly hours', () => {
    expect(hoursValid(defaultHours(16, 38))).toBe(true);
    expect(
      hoursValid([
        { weekday: 1, from_slot: 16, to_slot: 30 },
        { weekday: 1, from_slot: 28, to_slot: 38 },
      ]),
    ).toBe(false);
    expect(hoursValid([{ weekday: 8, from_slot: 16, to_slot: 30 }])).toBe(false);
    expect(hoursValid([{ weekday: 2, from_slot: 30, to_slot: 30 }])).toBe(false);
  });

  it('knows the fields of each formula kind and the licence rule', () => {
    expect(formulaFields('half_day')).toMatchObject({ band: true, slots: false });
    expect(formulaFields('hourly')).toMatchObject({ slots: true });
    expect(formulaFields('week')).toMatchObject({ days: true, minDays: 7 });
    expect(licenceRequired('threshold', 40000, 30000)).toBe(true);
    expect(licenceRequired('threshold', 30000, 30000)).toBe(false);
    expect(licenceRequired('never', 400000, 30000)).toBe(false);
    expect(licenceRequired('always', 0, 30000)).toBe(true);
  });

  it('builds the coverage probes and reads decimals', () => {
    expect(probeRange('hourly', '2027-06-12', 16, 38, undefined)).toEqual({
      start: { date: '2027-06-12', slot: 16 },
      end: { date: '2027-06-12', slot: 18 },
    });
    expect(probeRange('week', '2027-06-12', 16, 38, undefined)?.end).toEqual({
      date: '2027-06-18',
      slot: 38,
    });
    expect(probeRange('half_day', '2027-06-12', 16, 38, undefined)).toBeNull();
    expect(probeRange('half_day', '2027-06-12', 16, 38, morning)?.start.slot).toBe(16);
    expect(toMilli('60')).toBe(60000);
    expect(toMilli('22,5')).toBe(22500);
    expect(toMilli('x')).toBeNull();
  });
});

const unit = {
  id: 'u1',
  vessel_id: 'v1',
  vessel_name: 'Gabbiano',
  code: 'GOM58',
  category: 'rib',
  seats: 8,
  licence: 'threshold',
  licence_required: true,
  engine_kw_milli: 40000,
  fuel_type: 'petrol',
  tank_milli: 60000,
  deposit_cents: 50000,
  status: 'available',
  home_berth_id: 'b1',
  home_berth_code: 'A1',
  item_id: 'i1',
  ownership: 'marina',
  engine_hours_milli: 0,
  note: '',
  sort: 0,
  version: 1,
  created_at: '',
  updated_at: '',
};

const settings = {
  open_from_slot: 16,
  open_to_slot: 38,
  bands: [
    { ...morning, label: { it: 'Mattina' } },
    { ...afternoon, label: { it: 'Pomeriggio' } },
  ],
  buffer_slots: 1,
  desk_hold_minutes: 60,
  no_show_after_minutes: 60,
  licence_threshold_kw_milli: 30000,
  checklist: [{ key: 'hull', label: { it: 'Scafo' }, categories: [] }],
  checkout_min_photos: 2,
  checkin_min_photos: 1,
  deposit_modes: ['cash', 'card_pos'],
  identity_retention_days: 365,
  preauth_available: false,
  version: 0,
};

function routes(perms: string[]) {
  return [
    ...baseRoutes(makeMe({}, perms as never)),
    { path: `${API}/rental/units`, body: { items: [unit], next_cursor: null } },
    { path: `${API}/rental/settings`, method: 'GET', body: settings },
    { path: `${API}/rental/settings`, method: 'PUT', body: { ...settings, version: 1 } },
    { path: `${API}/rental/formulas`, body: { items: [] } },
    { path: `${API}/rental/extras`, body: { items: [] } },
    { path: `${API}/rental/skippers`, body: { items: [], next_cursor: null } },
    { path: `${API}/items`, body: { items: [], next_cursor: null } },
    { path: `${API}/revenue-classes`, body: { items: [] } },
    { path: `${API}/cash-registers`, body: { items: [] } },
  ];
}

describe('rental fleet page', () => {
  it('lists the units with the licence and status badges', async () => {
    renderApp(`/m/${MARINA_ID}/rental/fleet`, routes(['marina.read', 'rental.manage']));
    const row = await screen.findByTestId('unit-row');
    expect(within(row).getByText('GOM58')).toBeInTheDocument();
    expect(within(row).getByText('Gabbiano')).toBeInTheDocument();
    expect(within(row).getByText('Patente richiesta')).toBeInTheDocument();
    expect(within(row).getByText('A1')).toBeInTheDocument();
    expect(within(row).getByText('Disponibile')).toBeInTheDocument();
  });

  it('refuses access without rental.manage', async () => {
    renderApp(`/m/${MARINA_ID}/rental/fleet`, routes(['marina.read']));
    expect(await screen.findByText('Non hai accesso a questa sezione.')).toBeInTheDocument();
  });

  it('saves the settings with If-Match 0 on the first save and blocks a band gap below the buffer', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/rental/fleet`,
      routes(['marina.read', 'rental.manage', 'marina.configure']),
    );
    fireEvent.mouseDown(await screen.findByTestId('fleet-tab-settings'));
    fireEvent.click(screen.getByTestId('fleet-tab-settings'));
    const form = await screen.findByTestId('rental-settings');
    expect(within(form).getByTestId('band-strip')).toBeInTheDocument();
    const buffer = within(form).getByLabelText("Margine tra noleggi (mezz'ore)");
    fireEvent.change(buffer, { target: { value: '3' } });
    expect(await within(form).findByTestId('window-issues')).toHaveTextContent('margine');
    expect(within(form).getByTestId('settings-save')).toBeDisabled();
    fireEvent.change(buffer, { target: { value: '1' } });
    fireEvent.click(within(form).getByTestId('settings-save'));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.headers.get('If-Match')).toBe('"0"');
    expect(put?.body).toMatchObject({
      buffer_slots: 1,
      open_from_slot: 16,
      deposit_modes: ['cash', 'card_pos'],
    });
  });
});
