import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { MockRoute } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { encodeProbe } from './probe';
import {
  API,
  LIST_ID,
  breakdown,
  coverage,
  item,
  matrix,
  meWithPricing,
  multihullBreakdown,
  priceList,
  probe,
  service,
} from './test/fixtures';

vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

const common: MockRoute[] = [
  meRoute(meWithPricing()),
  { path: `${API}/items`, body: { items: [item, service], next_cursor: null } },
  { path: `${API}/price-lists`, body: { items: [] } },
  { path: `${API}/price-lists/${LIST_ID}`, body: priceList },
  { path: `${API}/berth-categories`, body: { items: [], unknown_in_use: [] } },
];

const simulatorAt = (p: Parameters<typeof encodeProbe>[0]) =>
  `/m/${MARINA_ID}/pricing/simulator?probe=${encodeProbe(p)}`;

const simulateCalls = (calls: { method: string; url: URL; body: unknown }[]) =>
  calls.filter((c) => c.method === 'POST' && c.url.pathname === `${API}/pricing/simulate`);

describe('SimulatorPage', () => {
  it('prices a probe from the URL and renders lines, VAT and trace', async () => {
    const req = {
      ...probe(1, 1),
      vessel: { loa_cm: 1134, beam_cm: 380, hull_type: 'monohull_motor' as const },
    };
    const { calls } = renderApp(simulatorAt(req), [
      ...common,
      { method: 'POST', path: `${API}/pricing/simulate`, body: breakdown },
    ]);
    expect(await screen.findByTestId('breakdown-total')).toHaveTextContent('256,50 €');
    // The exact probe reaches the server: same input, same engine call (INV-23).
    expect(simulateCalls(calls)[0]?.body).toEqual(req);

    const line = screen.getByTestId('line-1');
    expect(within(line).getByText('TRN-NTT · Transito notte')).toBeInTheDocument();
    expect(within(line).getByText('210,25 €')).toBeInTheDocument();
    expect(within(line).getByText('IVA inclusa')).toBeInTheDocument();
    const block = screen.getByTestId('block-1-0');
    expect(within(block).getByText('ALTA')).toBeInTheDocument();
    expect(
      await within(block).findByRole('link', { name: 'Transito alta al metro' }),
    ).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/price-lists/${LIST_ID}#rule-0190a000-0000-7000-8000-0000000b2001`,
    );
    const trace = screen.getByTestId('breakdown-trace');
    expect(within(trace).getByText('Lunghezza arrotondata')).toBeInTheDocument();
    expect(
      within(trace).getByText(/da: 11,34 m · a: 11,40 m · passo: 0,10 m · modo: Per eccesso/),
    ).toBeInTheDocument();
    expect(within(trace).getByText(/aliquota: 22% · lordo: 256,50 €/)).toBeInTheDocument();
    expect(screen.getByTestId('breakdown-resolved')).toHaveTextContent(
      'Indicate nella simulazione',
    );
    expect(screen.getByLabelText('Lunghezza fuori tutto')).toHaveValue('11,34');
  });

  it('sends the edited form as integers', async () => {
    const { calls } = renderApp(simulatorAt(probe(1, 1)), [
      ...common,
      { method: 'POST', path: `${API}/pricing/simulate`, body: breakdown },
    ]);
    await screen.findByTestId('breakdown-total');
    const loa = screen.getByLabelText('Lunghezza fuori tutto');
    await userEvent.clear(loa);
    await userEvent.type(loa, '11,34');
    await userEvent.type(screen.getByLabelText('Codice promozionale'), 'estate');
    await userEvent.click(screen.getByRole('button', { name: 'Calcola' }));
    await waitFor(() => expect(simulateCalls(calls)).toHaveLength(2));
    expect(simulateCalls(calls)[1]?.body).toEqual({
      ...probe(1, 1),
      vessel: { loa_cm: 1134, beam_cm: 400, hull_type: 'monohull_motor' },
      promo_code: 'ESTATE',
    });
  });

  it('shows the multihull modifier in the blocks and the trace, and the warnings', async () => {
    renderApp(
      simulatorAt({
        ...probe(1, 1),
        vessel: { loa_cm: 1200, beam_cm: 600, hull_type: 'catamaran' },
      }),
      [...common, { method: 'POST', path: `${API}/pricing/simulate`, body: multihullBreakdown }],
    );
    expect(await screen.findByTestId('breakdown-total')).toHaveTextContent('384,75 €');
    const block = screen.getByTestId('block-1-0');
    expect(await within(block).findByRole('link', { name: 'Multiscafo +50%' })).toBeInTheDocument();
    expect(
      within(screen.getByTestId('breakdown-trace')).getByText(
        /Maggiorazione o riduzione applicata/,
      ),
    ).toHaveTextContent('Multiscafo +50%');
    expect(screen.getByText(/variazione: 128,25 € · cumulabile: sì/)).toBeInTheDocument();
    expect(screen.getByText(/Il soggiorno supera la validità del listino/)).toBeInTheDocument();
    expect(screen.getByText(/Multiscafo: le maggiorazioni/)).toBeInTheDocument();
  });

  it('shows the no-price reason from the problem details', async () => {
    renderApp(simulatorAt(probe(0, 0)), [
      ...common,
      {
        method: 'POST',
        path: `${API}/pricing/simulate`,
        status: 422,
        body: {
          type: 'no_price',
          title: 'No price',
          status: 422,
          details: {
            contract_type: 'transit',
            night: '2027-01-01',
            category: 'L10',
            reason: 'no_rule',
          },
        },
      },
    ]);
    const alert = await screen.findByTestId('simulator-no-price');
    expect(within(alert).getByTestId('no-price-reason')).toHaveTextContent(
      'Nessuna regola di prezzo applicabile',
    );
    expect(alert).toHaveTextContent('Notte: 01/01/2027');
    expect(alert).toHaveTextContent('Categoria: L10');
    expect(screen.queryByTestId('breakdown')).toBeNull();
  });

  it('says when the promo code does not apply', async () => {
    renderApp(simulatorAt({ ...probe(1, 1), promo_code: 'NOPE' }), [
      ...common,
      {
        method: 'POST',
        path: `${API}/pricing/simulate`,
        status: 422,
        body: { type: 'promo_not_applicable', title: 'Promo', status: 422 },
      },
    ]);
    expect(await screen.findByTestId('simulator-promo')).toHaveTextContent(
      'Il codice promozionale non è applicabile a questa richiesta.',
    );
  });

  it('ignores a malformed probe and waits for the form', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/pricing/simulator?probe=xyz`, common);
    expect(await screen.findByText(/non contiene una simulazione valida/)).toBeInTheDocument();
    expect(screen.getByText(/premi Calcola/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Calcola' }));
    expect(await screen.findAllByText('Campo obbligatorio.')).not.toHaveLength(0);
    expect(simulateCalls(calls)).toHaveLength(0);
  });

  it('is forbidden without pricing.read', async () => {
    renderApp(`/m/${MARINA_ID}/pricing/simulator`, [meRoute(makeMe({}, ['marina.read']))]);
    expect(await screen.findByText(/Non hai accesso a questa sezione/)).toBeInTheDocument();
  });
});

describe('MatrixPage', () => {
  it('renders the matrix and opens a cell in the simulator with its probe', async () => {
    const { calls, router } = renderApp(`/m/${MARINA_ID}/price-lists/${LIST_ID}/matrix`, [
      ...common,
      { path: `${API}/price-lists/${LIST_ID}/matrix`, body: matrix },
      {
        method: 'POST',
        path: `${API}/pricing/simulate`,
        body: { ...breakdown, total_cents: 9000 },
      },
    ]);
    expect(
      await screen.findByRole('heading', { name: 'Matrice del listino T-2027' }),
    ).toBeInTheDocument();
    expect(await screen.findByTestId('matrix-cell-1-1')).toHaveTextContent('90,00 €');
    const get = calls.find((c) => c.url.pathname === `${API}/price-lists/${LIST_ID}/matrix`)!;
    expect(Object.fromEntries(get.url.searchParams)).toEqual({
      rows: 'bands',
      contract_type: 'transit',
    });
    expect(screen.getByRole('tab', { name: 'Transito' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('link', { name: /Stampa/ })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/price-lists/${LIST_ID}/matrix/print?rows=bands&contract_type=transit`,
    );

    await userEvent.click(
      screen.getByRole('button', { name: 'L12 × ALTA: 90,00 €, apri nel simulatore' }),
    );
    expect(await screen.findByTestId('breakdown-total')).toHaveTextContent('90,00 €');
    expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}/pricing/simulator`);
    expect(simulateCalls(calls)[0]?.body).toEqual(matrix.cells[3]!.probe);
  });

  it('switches contract type through the tabs', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/price-lists/${LIST_ID}/matrix`, [
      ...common,
      { path: `${API}/price-lists/${LIST_ID}/matrix`, body: matrix },
    ]);
    await screen.findByTestId('matrix-cell-1-1');
    await userEvent.click(screen.getByRole('tab', { name: 'Annuale' }));
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.url.pathname === `${API}/price-lists/${LIST_ID}/matrix` &&
            c.url.searchParams.get('contract_type') === 'annual',
        ),
      ).toBe(true),
    );
  });

  it('prints every contract type with the brand and the validity', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/price-lists/${LIST_ID}/matrix/print`, [
      ...common,
      { path: `${API}/price-lists/${LIST_ID}/matrix`, body: matrix },
    ]);
    const sheet = await screen.findByTestId('matrix-print');
    expect(within(sheet).getByRole('heading', { name: 'Listino T-2027' })).toBeInTheDocument();
    expect(within(sheet).getByTestId('print-validity')).toHaveTextContent(
      'Valido dal 01/01/2027 al 31/12/2027',
    );
    expect(within(sheet).getAllByAltText('Porto').length).toBeGreaterThan(0);
    expect(within(sheet).getByRole('heading', { name: 'Transito' })).toBeInTheDocument();
    expect(within(sheet).getByRole('heading', { name: 'Annuale' })).toBeInTheDocument();
    await waitFor(() =>
      expect(
        calls
          .filter((c) => c.url.pathname === `${API}/price-lists/${LIST_ID}/matrix`)
          .map((c) => c.url.searchParams.get('contract_type'))
          .sort(),
      ).toEqual(['annual', 'transit']),
    );
  });
});

describe('CoveragePage', () => {
  it('shows the summary, the gaps with their reasons and the links', async () => {
    renderApp(`/m/${MARINA_ID}/price-lists/${LIST_ID}/coverage`, [
      ...common,
      { path: `${API}/price-lists/${LIST_ID}/coverage`, body: coverage },
    ]);
    expect(await screen.findByTestId('coverage-gaps')).toHaveTextContent('3');
    expect(screen.getByTestId('coverage-checked')).toHaveTextContent('12');
    expect(screen.getByTestId('coverage-without-category')).toHaveTextContent('4');
    expect(screen.getByText('Posti 3, aree 1, risorse a terra 0')).toBeInTheDocument();
    expect(screen.getByText('L30 (2)')).toBeInTheDocument();
    const rows = screen.getAllByTestId('coverage-gap');
    expect(rows).toHaveLength(3);
    expect(within(rows[1]!).getByText('Nessuna fascia per questa lunghezza')).toBeInTheDocument();
    expect(within(rows[0]!).getByRole('link', { name: 'Apri regole' })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/price-lists/${LIST_ID}#rules`,
    );
    expect(screen.getByRole('link', { name: 'Apri le mappe' })).toBeInTheDocument();
  });

  it('says when every combination has a price', async () => {
    renderApp(`/m/${MARINA_ID}/price-lists/${LIST_ID}/coverage`, [
      ...common,
      {
        path: `${API}/price-lists/${LIST_ID}/coverage`,
        body: {
          ...coverage,
          gaps: [],
          unknown_categories: [],
          elements_without_category: { berths: 0, dynamic_areas: 0, land_resources: 0 },
        },
      },
    ]);
    expect(await screen.findByText('Ogni combinazione ha un prezzo')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Apri le mappe' })).toBeNull();
  });
});
