import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { OnlineBookingSettings } from '@porto/shared/api';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import '../pricing/test/dom';
import { embedSnippets, formatPairs, parsePairs } from './lib/constants';
import { settingsSchema, toInput, toValues } from './lib/settings';

const M = `/api/admin/v1/marinas/${MARINA_ID}`;

const settings: OnlineBookingSettings = {
  rules: {
    min_nights: 1,
    max_nights: 30,
    min_advance_hours: 2,
    max_advance_days: 365,
    arrival_from: '12:00',
    arrival_to: '20:00',
    closed_weekdays: [],
  },
  payment_mode: 'full',
  deposit_bp: 3000,
  cancellation_tiers: [
    { min_days_before: 7, refund_bp: 10000 },
    { min_days_before: 2, refund_bp: 5000 },
    { min_days_before: 0, refund_bp: 0 },
  ],
  refund_as: 'refund',
  approval: { always: false, group_ids: [], hold_hours: 48 },
  required_fields: {
    documents: ['insurance_policy'],
    people_on_board: true,
    phone: true,
    documents_later: false,
  },
  reveal_berth: 'after_confirmation',
  late_payment_recovery: true,
  allow_without_provider: false,
  max_live_holds: 50,
  theme_preset: 'default',
  hide_header_in_embed: false,
  show_feet: true,
  saved: false,
  version: 0,
  booking_enabled: true,
  embed_origins: ['https://www.marina-prova.example'],
};

const admin = makeMe({}, ['marina.read', 'marina.configure', 'harbor.configure', 'map.view']);

function routes() {
  return [
    meRoute(admin),
    { path: `${M}/online-booking/settings`, body: settings },
    {
      method: 'PUT',
      path: `${M}/online-booking/settings`,
      body: { ...settings, saved: true, version: 1 },
    },
    { path: `${M}/berth-groups`, body: { items: [] } },
    { path: `${M}/online-booking/texts`, body: { items: [] } },
  ];
}

describe('OnlineSettingsPage', () => {
  it('round-trips the settings through the form values', () => {
    const v = toValues(settings);
    expect(settingsSchema.safeParse(v).success).toBe(true);
    const input = toInput(v);
    expect(input.cancellation_tiers).toHaveLength(3);
    expect(input.approval.loa_over_cm).toBeUndefined();
    expect(input.arrival_info).toEqual({});
  });

  it('refuses duplicate tiers and min nights above max', () => {
    const v = toValues(settings);
    v.cancellation_tiers = [
      { min_days_before: 3, refund_bp: 100 },
      { min_days_before: 3, refund_bp: 50 },
    ];
    expect(settingsSchema.safeParse(v).success).toBe(false);
    const w = toValues(settings);
    w.rules.min_nights = 40;
    expect(settingsSchema.safeParse(w).success).toBe(false);
  });

  it('parses key=value lines and builds the embed snippets', () => {
    expect(parsePairs('a=1\n\nnoequals\n b = x=y ')).toEqual({ a: '1', b: 'x=y' });
    expect(formatPairs({ A: 'standard' })).toBe('A=standard');
    const s = embedSnippets('https://porto.example', 'prova', 'it');
    expect(s.script).toContain('data-porto="prova"');
    expect(s.script).toContain('https://porto.example/embed.js');
    expect(s.iframe).toContain('src="https://porto.example/b/prova?embed=1&lang=it"');
    expect(s.iframe).toContain('allow="payment"');
    expect(s.link).toBe('https://porto.example/b/prova');
  });

  it('saves the defaults the first time with If-Match 0 after editing a tier', async () => {
    const user = userEvent.setup();
    const { calls } = renderApp(`/m/${MARINA_ID}/settings/online`, routes());
    expect(await screen.findByTestId('online-settings-defaults')).toBeInTheDocument();
    await user.click(screen.getByRole('tab', { name: 'Pagamento e cancellazione' }));
    const tiers = await screen.findByTestId('online-tiers');
    await user.click(within(tiers).getByTestId('online-add-tier'));
    // The new tier duplicates "0 days before": the form refuses to save.
    const saves = screen.getAllByTestId('online-settings-save');
    await user.click(saves[saves.length - 1] as HTMLElement);
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(false));
    const days = within(tiers).getAllByLabelText('Almeno giorni prima');
    await user.clear(days[3] as HTMLElement);
    await user.type(days[3] as HTMLElement, '30');
    await user.click(saves[saves.length - 1] as HTMLElement);
    await waitFor(() => {
      const put = calls.find((c) => c.method === 'PUT');
      expect(put?.headers.get('If-Match')).toBe('"0"');
      expect((put?.body as { cancellation_tiers: unknown[] }).cancellation_tiers).toHaveLength(4);
    });
  });

  it('shows the allowed origins and the snippets in the embed tab', async () => {
    const user = userEvent.setup();
    renderApp(`/m/${MARINA_ID}/settings/online`, routes());
    await user.click(await screen.findByRole('tab', { name: 'Incorpora' }));
    expect(await screen.findByTestId('embed-origins')).toHaveTextContent(
      'https://www.marina-prova.example',
    );
    expect((screen.getByLabelText('Codice con script') as HTMLTextAreaElement).value).toContain(
      'data-porto="prova"',
    );
  });
});
