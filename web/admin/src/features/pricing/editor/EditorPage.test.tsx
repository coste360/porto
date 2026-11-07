import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { PriceListSave } from '@porto/shared/api';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { pickOption } from '../test/dom';
import { base, makeDocument, pricingRoutes } from '../test/fixtures';

const path = `/m/${MARINA_ID}/price-lists/pl1`;
const docPath = `${base}/price-lists/pl1`;

function render(doc = makeDocument(), extra: Parameters<typeof renderApp>[1] = []) {
  return renderApp(path, [...extra, { path: docPath, body: doc }, ...pricingRoutes()]);
}

async function openTab(name: RegExp) {
  await userEvent.click(await screen.findByRole('tab', { name }));
}

describe('PriceListEditorPage', () => {
  it('shows bands with their computed "da–a" in metres', async () => {
    render();
    expect(await screen.findByLabelText('Codice')).toHaveValue('T-2027');
    await openTab(/Fasce/);
    expect(screen.getByTestId('band-range-0')).toHaveTextContent('da 0,00 a 10,00 m');
    expect(screen.getByTestId('band-range-1')).toHaveTextContent('da 10,01 a 12,00 m');
    // A new band opens after the last one.
    await userEvent.click(screen.getByRole('button', { name: /Aggiungi fascia/ }));
    expect(screen.getByTestId('band-range-2')).toHaveTextContent('da 12,01 a 14,00 m');
  });

  it('renders the period timeline with the period colours', async () => {
    render();
    await openTab(/Periodi/);
    const timeline = screen.getByTestId('period-timeline');
    const june1 = timeline.querySelector('[data-date="2027-06-01"]')!;
    expect(june1).toHaveAttribute('data-period', 'ALTA');
    expect(june1.className).toContain('bg-chart-5');
    expect(timeline.querySelector('[data-date="2027-01-01"]')!.className).toContain('bg-chart-3');
  });

  it('edits a rule in the Sheet and saves the whole document with If-Match and ids', async () => {
    const saved = makeDocument({ version: 6 });
    const { calls } = render(makeDocument(), [
      {
        method: 'PUT',
        path: docPath,
        body: { document: saved, id_map: {}, issues: [] },
      },
    ]);
    await openTab(/Regole/);
    await userEvent.click(screen.getByRole('button', { name: 'Transito alta' }));
    const sheet = await screen.findByRole('dialog');
    const price = within(sheet).getByLabelText('Prezzo al metro');
    expect(price).toHaveValue('7,50');
    fireEvent.change(price, { target: { value: '8' } });
    await userEvent.click(within(sheet).getByRole('button', { name: 'Applica' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(screen.getByText('Modifiche non salvate.')).toBeInTheDocument();
    // New modifier: it gets a client key.
    await userEvent.click(screen.getByRole('button', { name: /Aggiungi maggiorazione/ }));
    const sheet2 = await screen.findByRole('dialog');
    await userEvent.click(within(sheet2).getByRole('button', { name: 'Applica' }));
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.headers.get('If-Match')).toBe('"5"');
    const body = put.body as PriceListSave;
    expect(body.rules[0]).toMatchObject({
      id: 'r1',
      unit_cents: 800,
      scope: { periods: ['ALTA'] },
    });
    expect(body.rules[0]).not.toHaveProperty('seq');
    expect(body.rules[1]).toMatchObject({ stage: 'modifier', effect: { percent_bp: 1000 } });
    expect(body.rules[1]!.key).toMatch(/^r:/);
    expect(body.rules[1]).not.toHaveProperty('id');
    expect(body.periods.map((p) => p.id)).toEqual(['p1', 'p2']);
    expect(await screen.findByText('Listino salvato.')).toBeInTheDocument();
  });

  it('the rule Sheet shows the fields of the chosen basis and parses money to cents', async () => {
    render();
    await openTab(/Regole/);
    await userEvent.click(screen.getByRole('button', { name: /Aggiungi prezzo base/ }));
    const sheet = await screen.findByRole('dialog');
    expect(within(sheet).getByLabelText('Prezzo al metro')).toBeInTheDocument();
    expect(within(sheet).queryByTestId('amounts-grid')).not.toBeInTheDocument();
    await pickOption('Base di calcolo', 'Per fascia di lunghezza', sheet);
    const grid = within(sheet).getByTestId('amounts-grid');
    expect(within(grid).getByText('da 10,01 a 12,00 m')).toBeInTheDocument();
    expect(within(sheet).queryByLabelText('Prezzo al metro')).not.toBeInTheDocument();
    fireEvent.change(within(grid).getByLabelText('Importo per L12'), {
      target: { value: '1.234,56' },
    });
    await pickOption('Base di calcolo', 'Importo fisso', sheet);
    expect(within(sheet).queryByTestId('amounts-grid')).not.toBeInTheDocument();
    expect(within(sheet).getByLabelText('Importo')).toBeInTheDocument();
    await pickOption('Base di calcolo', 'Per fascia di lunghezza', sheet);
    // The grid is rebuilt for the basis: amounts typed for another basis are not kept.
    expect(within(sheet).getByLabelText('Importo per L12')).toHaveValue('');
    // Modifiers have effect and conditions, no basis.
    await userEvent.click(within(sheet).getByRole('button', { name: 'Chiudi' }));
    await userEvent.click(screen.getByRole('button', { name: /Aggiungi sconto/ }));
    const disc = await screen.findByRole('dialog');
    expect(within(disc).queryByLabelText('Base di calcolo')).not.toBeInTheDocument();
    expect(within(disc).getByLabelText('Sconto percentuale')).toHaveValue('10');
    await pickOption('Tipo di sconto', 'Codice promozionale', disc);
    expect(within(disc).getByLabelText('Codice promozionale')).toBeInTheDocument();
  });

  it('a 412 opens the conflict dialog and reload adopts the server version', async () => {
    const newer = makeDocument({ version: 9, code: 'T-2027-SRV' });
    let putDone = false;
    const { calls } = renderApp(path, [
      {
        method: 'PUT',
        path: (u) => {
          putDone = u.pathname === docPath;
          return putDone;
        },
        status: 412,
        body: { type: 'version_conflict', title: 'Conflict', status: 412 },
      },
      {
        path: (u) => putDone && u.pathname === docPath,
        body: newer,
      },
      { path: docPath, body: makeDocument() },
      ...pricingRoutes(),
    ]);
    const code = await screen.findByLabelText('Codice');
    fireEvent.change(code, { target: { value: 'T-2027-LOC' } });
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(await within(dialog).findByTestId('conflict-details')).toHaveTextContent(
      'Versione salvata 9',
    );
    await userEvent.click(within(dialog).getByRole('button', { name: 'Ricarica' }));
    await waitFor(() => expect(screen.getByLabelText('Codice')).toHaveValue('T-2027-SRV'));
    expect(calls.filter((c) => c.method === 'PUT')).toHaveLength(1);
  });

  it('an active list is read-only', async () => {
    render(makeDocument({ status: 'active' }));
    expect(await screen.findByText('Listino pubblicato')).toBeInTheDocument();
    expect(screen.getByLabelText('Codice')).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Salva' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Attiva' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Archivia' })).toBeInTheDocument();
    await openTab(/Regole/);
    expect(screen.queryByRole('button', { name: /Aggiungi prezzo base/ })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Transito alta' }));
    const sheet = await screen.findByRole('dialog');
    expect(within(sheet).queryByRole('button', { name: 'Applica' })).not.toBeInTheDocument();
    expect(within(sheet).getByLabelText('Prezzo al metro')).toBeDisabled();
  });

  it('shows validation issues and a click opens the rule', async () => {
    render(
      makeDocument({
        issues: [
          { code: 'rule_basis_invalid', severity: 'error', ref: { kind: 'rule', id: 'r1' } },
          { code: 'validity_not_covered_by_periods', severity: 'warning', ref: { kind: 'list' } },
        ],
      }),
    );
    await openTab(/Validazione/);
    expect(screen.getByText('Errori: 1')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Attiva' })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: /Base di calcolo non coerente/ }));
    const sheet = await screen.findByRole('dialog');
    expect(within(sheet).getByLabelText('Nome')).toHaveValue('Transito alta');
  });

  it('activates at a date and lists the truncated predecessors', async () => {
    const { calls } = render(makeDocument(), [
      {
        method: 'POST',
        path: `${docPath}/activate`,
        body: {
          document: makeDocument({ status: 'active', version: 6 }),
          truncated: [
            { id: 'old', code: 'T-2026', valid_from: '2026-01-01', valid_to: '2026-12-31' },
          ],
        },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Attiva' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Attiva' }));
    expect(await within(dialog).findByTestId('truncated')).toHaveTextContent(
      'T-2026: valido fino al 31/12/2026',
    );
    const post = calls.find((c) => c.method === 'POST' && c.url.pathname.endsWith('/activate'))!;
    expect(post.headers.get('If-Match')).toBe('"5"');
    expect(post.body).toEqual({});
  });

  it('an overlapping activation lists the conflicting lists', async () => {
    render(makeDocument(), [
      {
        method: 'POST',
        path: `${docPath}/activate`,
        status: 409,
        body: {
          type: 'price_list_overlap',
          title: 'Overlap',
          status: 409,
          details: {
            conflicts: [
              { id: 'x', code: 'T-ALT', valid_from: '2027-01-01', valid_to: '2027-12-31' },
            ],
          },
        },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Attiva' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Attiva' }));
    expect(
      await within(dialog).findByText(/T-ALT \(dal 01\/01\/2027 al 31\/12\/2027\)/),
    ).toBeInTheDocument();
  });
});
