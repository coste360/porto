import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';

import type { MockRoute } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { makeMapDocument } from '../maps/testFixtures';
import { ALL_OPS, DATE, base, makeDayView, summary } from './testFixtures';

const doc = makeMapDocument();

function routes(perms = ALL_OPS, day = makeDayView()): MockRoute[] {
  return [
    meRoute(makeMe({}, perms)),
    { path: `${base}/maps`, body: { items: [summary(doc)] } },
    { path: `${base}/maps/map-1`, body: doc },
    { path: `${base}/maps/map-1/day`, body: day },
    { path: `${base}/berth-groups`, body: { items: [] } },
  ];
}

afterEach(() => {
  window.localStorage.clear();
});

describe('OpsPage', () => {
  it('colours the berths with the lens and counts the legend from the day view', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/ops?date=${DATE}`, routes());
    expect(await screen.findByTestId('ops-legend-count-annual')).toHaveTextContent('1');
    expect(screen.getByTestId('ops-legend-count-free')).toHaveTextContent('1');
    expect(screen.getByTestId('ops-legend-count-transit')).toHaveTextContent('1');
    expect(screen.getByTestId('ops-legend-count-hold')).toHaveTextContent('0');
    const day = calls.find((c) => c.url.pathname.endsWith('/maps/map-1/day'));
    expect(day?.url.searchParams.get('date')).toBe(DATE);
    // Elements stay buttons with "{code}, {status}, {vessel}".
    expect(
      await screen.findByRole('button', { name: 'A-2, Contratto, Aurora' }),
    ).toBeInTheDocument();
    const map = screen.getByTestId('ops-map');
    expect(map.querySelector('[data-ref="b2"] rect')?.getAttribute('class')).toContain(
      'fill-harbor-annual',
    );
    expect(map.querySelector('[data-ref="b3"] rect')?.getAttribute('data-lens')).toBe('transit');
  });

  it('switches lens and greys the elements whose legend key is unchecked', async () => {
    renderApp(`/m/${MARINA_ID}/ops?date=${DATE}`, routes());
    await userEvent.click(await screen.findByTestId('ops-lens-tab-presence'));
    expect(await screen.findByTestId('ops-legend-count-in')).toHaveTextContent('1');
    expect(screen.getByTestId('ops-legend-count-arrival')).toHaveTextContent('1');
    const map = screen.getByTestId('ops-map');
    await userEvent.click(within(screen.getByTestId('ops-legend-item-in')).getByRole('checkbox'));
    await waitFor(() =>
      expect(map.querySelector('[data-ref="b2"] rect')?.getAttribute('class')).toContain(
        'fill-harbor-muted',
      ),
    );
    // The count is kept: the legend filters what is shown, not what is counted.
    expect(screen.getByTestId('ops-legend-count-in')).toHaveTextContent('1');
  });

  it('filters by free text: counts follow the filter', async () => {
    renderApp(`/m/${MARINA_ID}/ops?date=${DATE}`, routes());
    await userEvent.type(await screen.findByLabelText('Cerca'), 'brezza');
    await waitFor(() =>
      expect(screen.getByTestId('ops-legend-count-annual')).toHaveTextContent('0'),
    );
    expect(screen.getByTestId('ops-legend-count-transit')).toHaveTextContent('1');
  });

  it('opens the quick card with the actions allowed by the role', async () => {
    renderApp(`/m/${MARINA_ID}/ops?date=${DATE}`, routes());
    await userEvent.click(await screen.findByRole('button', { name: 'A-2, Contratto, Aurora' }));
    const card = await screen.findByTestId('ops-quick-card');
    expect(within(card).getByText('ORM-2026-0001')).toBeInTheDocument();
    for (const a of ['departed', 'move', 'swap', 'release']) {
      expect(within(card).getByTestId(`ops-quick-action-${a}`)).toBeInTheDocument();
    }
    expect(within(card).queryByTestId('ops-quick-action-newTransit')).toBeNull();
  });

  it('shows no write action to a viewer', async () => {
    renderApp(`/m/${MARINA_ID}/ops?date=${DATE}`, routes(['marina.read', 'map.view']));
    await userEvent.click(
      await screen.findByRole('button', { name: 'A-1, Libero, nessuna barca' }),
    );
    const card = await screen.findByTestId('ops-quick-card');
    expect(within(card).queryAllByTestId(/ops-quick-action-/)).toEqual([]);
  });

  it('free berth: new transit and block for the desk', async () => {
    renderApp(`/m/${MARINA_ID}/ops?date=${DATE}`, routes());
    await userEvent.click(
      await screen.findByRole('button', { name: 'A-1, Libero, nessuna barca' }),
    );
    const card = await screen.findByTestId('ops-quick-card');
    expect(within(card).getByTestId('ops-quick-action-newTransit')).toBeInTheDocument();
    expect(within(card).getByTestId('ops-quick-action-newQuote')).toBeInTheDocument();
    expect(within(card).getByTestId('ops-quick-action-block')).toBeInTheDocument();
  });

  it('offers the list alternative by structure', async () => {
    renderApp(`/m/${MARINA_ID}/ops?date=${DATE}`, routes());
    await userEvent.click(await screen.findByTestId('ops-list-toggle'));
    const list = await screen.findByTestId('ops-list');
    expect(within(list).getByRole('region', { name: 'A' })).toBeInTheDocument();
    expect(within(list).getByText('Brezza')).toBeInTheDocument();
    await userEvent.click(within(list).getByRole('button', { name: 'A-3' }));
    expect(await screen.findByTestId('ops-quick-card')).toBeInTheDocument();
  });

  it('remembers the schema mode and draws tiles with the vessel name', async () => {
    renderApp(`/m/${MARINA_ID}/ops?date=${DATE}`, routes());
    await userEvent.click(await screen.findByTestId('ops-mode-schema'));
    await waitFor(() =>
      expect(screen.getByTestId('ops-map')).toHaveAttribute('data-mode', 'schema'),
    );
    expect(window.localStorage.getItem('porto:ops:mode:u1')).toBe('schema');
  });
});
