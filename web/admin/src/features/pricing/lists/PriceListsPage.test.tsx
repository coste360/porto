import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { PriceListSummary } from '@porto/shared/api';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { base, makeDocument, pricingRoutes } from '../test/fixtures';

const summary = (over: Partial<PriceListSummary>): PriceListSummary => ({
  id: 'a',
  code: 'T-2027',
  name: { it: 'Transiti 2027' },
  status: 'active',
  valid_from: '2027-01-01',
  valid_to: '2027-12-31',
  contract_types: ['transit'],
  rule_count: 4,
  version: 2,
  ...over,
});

const lists = [
  summary({}),
  summary({
    id: 'b',
    code: 'T-2028',
    status: 'draft',
    valid_from: '2028-01-01',
    valid_to: '2028-12-31',
  }),
  summary({
    id: 'c',
    code: 'T-2026',
    status: 'archived',
    valid_from: '2026-01-01',
    valid_to: '2026-12-31',
  }),
];

function render(extra: Parameters<typeof renderApp>[1] = []) {
  return renderApp(`/m/${MARINA_ID}/price-lists`, [
    ...extra,
    { path: `${base}/price-lists`, body: { items: lists } },
    ...pricingRoutes(),
  ]);
}

describe('PriceListsPage', () => {
  it('groups lists by status and draws the validity timeline per contract type', async () => {
    render();
    const drafts = await screen.findByRole('region', { name: /Bozze/ });
    expect(within(drafts).getByRole('link', { name: 'T-2028' })).toBeInTheDocument();
    expect(
      within(screen.getByRole('region', { name: /Attivi/ })).getByText('T-2027'),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole('region', { name: /Archiviati/ })).getByText('T-2026'),
    ).toBeInTheDocument();
    const timeline = screen.getByTestId('validity-timeline');
    // Archived lists are not in the timeline; two years are shown.
    expect(within(timeline).getByText('2027')).toBeInTheDocument();
    expect(within(timeline).getByText('2028')).toBeInTheDocument();
    expect(within(timeline).queryByText('T-2026')).not.toBeInTheDocument();
    expect(
      within(timeline).getByRole('img', { name: 'T-2027: dal 01/01/2027 al 31/12/2027' }),
    ).toBeInTheDocument();
  });

  it('creates a draft and opens the editor', async () => {
    const { calls, router } = render([
      {
        method: 'POST',
        path: `${base}/price-lists`,
        status: 201,
        body: makeDocument({ id: 'pl1' }),
      },
      { path: `${base}/price-lists/pl1`, body: makeDocument() },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: /Nuovo listino/ }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Codice'), 'T-2027');
    await userEvent.type(within(dialog).getByLabelText('Nome (italiano)'), 'Transiti 2027');
    await userEvent.click(within(dialog).getByLabelText('Annuale'));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Crea' }));
    await waitFor(() => expect(router.state.location.pathname).toContain('/price-lists/pl1'));
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.body).toMatchObject({
      code: 'T-2027',
      name: { it: 'Transiti 2027' },
      contract_types: ['annual', 'transit'],
    });
  });

  it('deletes a draft with If-Match', async () => {
    const { calls } = render([{ method: 'DELETE', path: `${base}/price-lists/b`, status: 204 }]);
    await userEvent.click(await screen.findByRole('button', { name: 'Azioni per T-2028' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Elimina' }));
    const alert = await screen.findByRole('alertdialog');
    await userEvent.click(within(alert).getByRole('button', { name: 'Elimina' }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === 'DELETE')?.headers.get('If-Match')).toBe('"2"'),
    );
  });
});
