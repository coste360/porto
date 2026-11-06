import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { BerthGroup, HarborSettings } from '@porto/shared/api';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';

const base = `/api/admin/v1/marinas/${MARINA_ID}`;
const settings: HarborSettings = {
  version: 7,
  loa_tolerance_cm: 50,
  default_grid_cm: 50,
  price_class_rule: {
    basis: 'width',
    bands: [
      { max_cm: 350, class: 'L10' },
      { max_cm: 300, class: 'L8' },
    ],
    fallback: 'L25',
  },
};
const group: BerthGroup = {
  id: 'g1',
  code: 'PONT-A',
  name: { it: 'Pontile A' },
  kind: 'structure',
  sort_order: 0,
  member_counts: { berth: 80, dynamic_area: 0, land_resource: 0 },
  version: 2,
};
const owner = makeMe({}, ['marina.read', 'map.view', 'harbor.configure']);

function render(extra: Parameters<typeof renderApp>[1] = []) {
  return renderApp(`/m/${MARINA_ID}/harbor-settings`, [
    meRoute(owner),
    ...extra,
    { path: `${base}/harbor-settings`, body: settings },
    { path: `${base}/berth-groups`, body: { items: [group] } },
  ]);
}

describe('HarborSettingsPage', () => {
  it('saves the rule with sorted bands and If-Match', async () => {
    const { calls } = render([
      { method: 'PATCH', path: `${base}/harbor-settings`, body: { ...settings, version: 8 } },
    ]);
    const tolerance = await screen.findByLabelText('Tolleranza sulla lunghezza');
    expect(tolerance).toHaveValue('0,50');
    // The form shows the bands sorted by width.
    expect(screen.getAllByLabelText('Categoria').map((i) => (i as HTMLInputElement).value)).toEqual(
      ['L8', 'L10'],
    );
    fireEvent.change(tolerance, { target: { value: '1' } });
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.headers.get('If-Match')).toBe('"7"');
    expect(patch.body).toEqual({
      loa_tolerance_cm: 100,
      default_grid_cm: 50,
      price_class_rule: {
        basis: 'width',
        fallback: 'L25',
        bands: [
          { max_cm: 300, class: 'L8' },
          { max_cm: 350, class: 'L10' },
        ],
      },
    });
  });

  it('rejects two bands with the same width', async () => {
    const { calls } = render();
    await screen.findByLabelText('Tolleranza sulla lunghezza');
    const widths = screen.getAllByLabelText('Larghezza fino a');
    fireEvent.change(widths[1]!, { target: { value: '3' } });
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    expect(await screen.findByText('Due fasce hanno la stessa larghezza.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false);
  });

  it('lists, creates and deletes groups', async () => {
    const { calls } = render([
      {
        method: 'POST',
        path: `${base}/berth-groups`,
        status: 201,
        body: { ...group, id: 'g2', code: 'TRANSITI' },
      },
      { method: 'DELETE', path: `${base}/berth-groups/g1`, status: 204 },
    ]);
    expect(await screen.findByText('PONT-A')).toBeInTheDocument();
    expect(screen.getByText('80 elementi')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /Nuovo gruppo/ }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Codice'), 'TRANSITI');
    await userEvent.type(within(dialog).getByLabelText('Nome (italiano)'), 'Transiti');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva' }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === 'POST')?.body).toEqual({
        code: 'TRANSITI',
        name: { it: 'Transiti' },
        kind: 'custom',
      }),
    );
    await userEvent.click(screen.getByRole('button', { name: 'Elimina' }));
    const alert = await screen.findByRole('alertdialog');
    await userEvent.click(within(alert).getByRole('button', { name: 'Elimina' }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === 'DELETE')?.headers.get('If-Match')).toBe('"2"'),
    );
  });

  it('is forbidden without harbor.configure', async () => {
    renderApp(`/m/${MARINA_ID}/harbor-settings`, [
      meRoute(makeMe({}, ['marina.read', 'map.view'])),
    ]);
    expect(await screen.findByText(/Non hai accesso/)).toBeInTheDocument();
  });
});
