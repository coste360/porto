import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, baseRoutes } from '../../desk/test/fixtures';
import { AREA_ID, indicators, plan } from '../test/fixtures';

const render = (permissions: Parameters<typeof makeMe>[1]) =>
  renderApp(`/m/${MARINA_ID}/storage/${AREA_ID}?date=2026-11-20`, [
    ...baseRoutes(makeMe({}, permissions)),
    { path: `${API}/storage/areas/${AREA_ID}/plan`, body: plan },
    {
      method: 'POST',
      path: `${API}/storage/areas/${AREA_ID}/evaluate`,
      body: { indicators, access: [], issues: [] },
    },
    {
      method: 'POST',
      path: `${API}/storage/areas/${AREA_ID}/arrange`,
      body: {
        items: [
          { allocation_id: 'al-a', x_cm: 1500, y_cm: 600, rotation_dd: 0 },
          { allocation_id: 'al-b', x_cm: 1000, y_cm: 500, rotation_dd: 0 },
        ],
        indicators,
        access: [],
        seed: 7,
        iterations: 100,
        stopped_by: 'optimal',
      },
    },
    { method: 'POST', path: `${API}/dynamic-areas/${AREA_ID}/layout`, body: { moved: [] } },
  ]);

const boat = (id: string) =>
  screen.getByTestId('storage-plan').querySelector(`[data-allocation="${id}"]`)!;

describe('StoragePlannerPage', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('shows the plan with the Accessibilità lens: blocked and blocking boats and the day warning', async () => {
    render(['marina.read', 'storage.read']);
    await screen.findByTestId('storage-plan');
    expect(boat('al-a')).toHaveAttribute('data-lens', 'blocked');
    expect(boat('al-b')).toHaveAttribute('data-lens', 'blocking');
    expect(screen.getByTestId('storage-warnings')).toHaveTextContent(
      'Aria non può uscire: bloccata da Bora',
    );
    expect(
      screen.getByTestId('storage-timeline').querySelector('[data-exit-blocked="true"]'),
    ).toHaveAttribute('data-allocation', 'al-a');
    // Read only without storage.manage.
    expect(screen.queryByTestId('storage-save')).not.toBeInTheDocument();
  });

  it('moves a boat with the keyboard, evaluates the draft and saves only the changed boat', async () => {
    const { calls } = render(['marina.read', 'storage.read', 'storage.manage']);
    await screen.findByTestId('storage-plan');
    const b = boat('al-a') as HTMLElement;
    b.focus();
    fireEvent.keyDown(b, { key: 'ArrowRight' });
    fireEvent.keyDown(b, { key: 'ArrowDown' });
    await waitFor(
      () => expect(calls.some((c) => c.url.pathname.endsWith('/evaluate'))).toBe(true),
      { timeout: 3000 },
    );
    const ev = calls.find((c) => c.url.pathname.endsWith('/evaluate'))!.body as {
      items: { allocation_id: string; x_cm: number; y_cm: number }[];
    };
    expect(ev.items.find((i) => i.allocation_id === 'al-a')).toMatchObject({
      x_cm: 100,
      y_cm: 100,
    });
    const save = await screen.findByTestId('storage-save');
    await waitFor(() => expect(save).toBeEnabled());
    await userEvent.click(save);
    await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/layout'))).toBe(true));
    const body = calls.find((c) => c.url.pathname.endsWith('/layout'))!.body as {
      date: string;
      base: unknown[];
      placements: unknown[];
    };
    expect(body.date).toBe('2026-11-20');
    expect(body.base).toEqual([{ allocation_id: 'al-a', version: 3 }]);
    expect(body.placements).toEqual([
      { allocation_id: 'al-a', x_cm: 100, y_cm: 100, rotation_dd: 0 },
    ]);
  });

  it('Preset fills the draft with the proposal without saving', async () => {
    const { calls } = render(['marina.read', 'storage.read', 'storage.manage']);
    await screen.findByTestId('storage-plan');
    await userEvent.click(screen.getByRole('button', { name: 'Preset' }));
    await waitFor(() =>
      expect(
        within(screen.getByTestId('storage-plan')).getByRole('button', { name: /Aria/ }),
      ).toBeInTheDocument(),
    );
    await waitFor(() =>
      expect(
        boat('al-a').querySelector('rect:last-of-type') as SVGRectElement | null,
      ).not.toBeNull(),
    );
    expect(calls.some((c) => c.url.pathname.endsWith('/layout'))).toBe(false);
    expect(screen.getByTestId('storage-save')).toHaveTextContent('Salva (1)');
  });
});
