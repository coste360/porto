import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ComparisonResult } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, baseRoutes } from '../../desk/test/fixtures';
import { pickOption } from '../../pricing/test/dom';

const result: ComparisonResult = {
  indicator: 'occupancy',
  unit: 'bp',
  align: 'date',
  granularity: 'month',
  years_back: 1,
  filters: {},
  current: { from: '2026-01-01', to: '2026-02-28', value: 5000, numerator: 10, denominator: 20 },
  previous: { from: '2025-01-01', to: '2025-02-28', value: 2500, numerator: 5, denominator: 20 },
  delta: 2500,
  delta_bp: 10000,
  series: [
    { bucket: '2026-01-01', current: 4000, previous: 2000 },
    { bucket: '2026-02-01', current: 6000, previous: null },
  ],
};

const render = (permissions: Parameters<typeof makeMe>[1], search = '') =>
  renderApp(`/m/${MARINA_ID}/reports/comparison${search}`, [
    ...baseRoutes(makeMe({}, permissions)),
    {
      path: `${API}/reports/comparison`,
      body: result,
    },
  ]);

describe('ComparisonPage', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('maps the aligned series to the delta table with the totals row', async () => {
    const { calls } = render(
      ['marina.read', 'reports.all'],
      '?indicator=occupancy&from=2026-01-01&to=2026-02-28',
    );
    const rows = await screen.findAllByTestId('comparison-row');
    expect(rows.map((r) => [...r.querySelectorAll('td')].map((c) => c.textContent))).toEqual([
      [expect.stringMatching(/gen.*2026/), '40%', '20%', '20%'],
      [expect.stringMatching(/feb.*2026/), '60%', '—', '—'],
    ]);
    expect(screen.getByTestId('comparison-total')).toHaveTextContent('Totale50%25%25% (100%)');
    const q = calls.find((c) => c.url.pathname.endsWith('/reports/comparison'))!.url.searchParams;
    expect(q.get('indicator')).toBe('occupancy');
    expect(q.get('from')).toBe('2026-01-01');
    expect(q.get('format')).toBeNull();
  });

  it('keeps the controls in the search and asks the server again', async () => {
    const { calls, router } = render(['marina.read', 'reports.all']);
    await screen.findAllByTestId('comparison-row');
    // Defaults: first indicator, year to date.
    const first = calls.find((c) => c.url.pathname.endsWith('/reports/comparison'))!.url
      .searchParams;
    expect(first.get('indicator')).toBe('production');
    expect(first.get('from')).toBe('2026-01-01');
    expect(first.get('to')).toBe('2026-11-08');
    await userEvent.click(screen.getByRole('radio', { name: 'Per giorno della settimana' }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ align: 'weekday' }));
    await pickOption('Anni a confronto', '3 anni');
    await pickOption('Raggruppamento', 'Settimana');
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.url.searchParams.get('years_back') === '3' &&
            c.url.searchParams.get('align') === 'weekday' &&
            c.url.searchParams.get('granularity') === 'week',
        ),
      ).toBe(true),
    );
  });

  it('exports the same query as CSV', async () => {
    const { calls } = render(
      ['marina.read', 'reports.all'],
      '?indicator=charged&from=2026-01-01&to=2026-02-28',
    );
    await screen.findAllByTestId('comparison-row');
    await userEvent.click(screen.getByRole('button', { name: 'Esporta CSV' }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('format') === 'csv')).toBe(true),
    );
    const q = calls.find((c) => c.url.searchParams.get('format') === 'csv')!.url.searchParams;
    expect(q.get('indicator')).toBe('charged');
    expect(q.get('lang')).toBe('it');
  });

  it('offers only operational indicators without reports.all and refuses other roles', async () => {
    render(['marina.read', 'reports.operational'], '?indicator=production');
    await screen.findAllByTestId('comparison-row');
    const trigger = screen.getByRole('combobox', { name: 'Indicatore' });
    expect(trigger).toHaveTextContent('Occupazione');
    await userEvent.click(trigger);
    const options = await screen.findAllByRole('option');
    expect(options.map((o) => o.textContent)).toEqual([
      'Occupazione',
      'Efficienza allocativa',
      'Arrivi',
      'Partenze',
      'Richieste perse',
    ]);
  });

  it('shows the forbidden state to roles without report permissions', async () => {
    render(['marina.read']);
    expect(await screen.findByText('Non hai accesso a questa sezione.')).toBeInTheDocument();
    expect(within(document.body).queryByTestId('comparison-page')).not.toBeInTheDocument();
  });
});
