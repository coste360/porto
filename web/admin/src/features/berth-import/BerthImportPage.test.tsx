import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { BerthImportReport, MapDocument } from '@porto/shared/api';
import { problem } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';

vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

const MAP_ID = '0190a000-0000-7000-8000-0000000000b1';
const API = `/api/admin/v1/marinas/${MARINA_ID}`;
const me = makeMe({}, ['marina.read', 'map.view', 'harbor.configure']);

const map = {
  id: MAP_ID,
  version: 7,
  updated_at: '2026-11-01T10:00:00Z',
  kind: 'water',
  name: { it: 'Specchio acqueo' },
  icon: '',
  width_cm: 60000,
  height_cm: 40000,
  grid_cm: 50,
  sort_order: 0,
  background: null,
  georef: null,
  concession_outline: null,
  water_polygons: [],
  land_polygons: [],
  structures: [{ id: 's1', kind: 'pier', code: 'F', points: [] }],
  berths: [],
  dynamic_areas: [],
  land_resources: [],
  pedestals: [],
  decorations: [],
} as unknown as MapDocument;

const report: BerthImportReport = {
  rows_total: 3,
  valid: 2,
  rejected: [{ row: 3, errors: [{ field: 'length', code: 'invalid_length' }] }],
  duplicates: [],
  planned: [
    { row: 1, code: 'F-1', pier: 'F', side: 'left' },
    { row: 2, code: 'F-2', pier: 'F', side: 'right' },
  ],
};

const CSV = 'Codice;Pontile;Lunghezza;Larghezza\nF-1;F;12,5;4\nF-2;F;12,5;4\nF-3;G;x;4\n';

afterEach(() => {
  try {
    localStorage.clear();
  } catch {
    // ignore
  }
});

async function loadFile() {
  const input = await screen.findByLabelText('File CSV');
  await userEvent.upload(input, new File([CSV], 'posti.csv', { type: 'text/csv' }));
  expect(await screen.findByText('posti.csv: 3 righe di dati.')).toBeInTheDocument();
  await userEvent.click(screen.getByRole('button', { name: 'Continua' }));
  // Columns are guessed from the Italian headers.
  expect(await screen.findByText('Anteprima delle prime righe (3)')).toBeInTheDocument();
  await userEvent.click(screen.getByRole('button', { name: 'Continua' }));
  expect(await screen.findByText('G (non sulla mappa)')).toBeInTheDocument();
}

describe('BerthImportPage', () => {
  it('runs a dry run, shows the report and applies with If-Match', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/maps/${MAP_ID}/import-berths`, [
      meRoute(me),
      { path: `${API}/maps/${MAP_ID}`, body: map },
      {
        method: 'POST',
        path: `${API}/maps/${MAP_ID}/berth-import`,
        body: report,
      },
    ]);
    await loadFile();
    await userEvent.click(screen.getByRole('button', { name: 'Prova senza salvare' }));
    expect(await screen.findByText('Risultato della prova')).toBeInTheDocument();
    expect(screen.getByTestId('report-valid')).toHaveTextContent('2');
    expect(screen.getByTestId('report-rejected')).toHaveTextContent('1');
    expect(screen.getByText('invalid_length')).toBeInTheDocument();
    expect(screen.getByText('Posti da creare (2)')).toBeInTheDocument();

    const dry = calls.find((c) => c.method === 'POST')!;
    expect(dry.headers.get('If-Match')).toBeNull();
    expect(dry.body).toEqual({
      csv: CSV,
      delimiter: 'semicolon',
      has_header: true,
      mapping: { code: 0, pier: 1, length: 2, width: 3 },
      units: 'm',
      placement: { start: 'after_existing', side_policy: 'alternate' },
      dry_run: true,
    });

    await userEvent.click(screen.getByRole('button', { name: 'Importa' }));
    expect(await screen.findByText('Importazione completata')).toBeInTheDocument();
    const posts = calls.filter((c) => c.method === 'POST');
    expect(posts).toHaveLength(2);
    expect(posts[1]!.headers.get('If-Match')).toBe('"7"');
    expect((posts[1]!.body as { dry_run: boolean }).dry_run).toBe(false);
    expect(screen.getByRole('link', { name: "Apri la mappa nell'editor" })).toBeInTheDocument();

    // The mapping is remembered per marina by header name (no file content).
    expect(JSON.parse(localStorage.getItem(`porto.berth-import.mapping.${MARINA_ID}`)!)).toEqual({
      code: 'Codice',
      pier: 'Pontile',
      length: 'Lunghezza',
      width: 'Larghezza',
    });
  });

  /** The first POST (dry run) answers the report, the next ones `failure`. */
  const routesFailingApply = (failure: { status: number; body: unknown }) => {
    let posts = 0;
    const importPath = `${API}/maps/${MAP_ID}/berth-import`;
    return [
      meRoute(me),
      { path: `${API}/maps/${MAP_ID}`, body: map },
      {
        method: 'POST',
        path: (url: URL) => url.pathname === importPath && posts++ === 0,
        body: report,
      },
      { method: 'POST', path: importPath, ...failure },
    ];
  };

  it('asks to reload on a version conflict', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/maps/${MAP_ID}/import-berths`,
      routesFailingApply({ status: 412, body: problem('version_conflict', 412) }),
    );
    await loadFile();
    await userEvent.click(screen.getByRole('button', { name: 'Prova senza salvare' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Importa' }));
    expect(
      await screen.findByText(/La mappa è stata modificata nel frattempo/),
    ).toBeInTheDocument();
    const mapLoads = () =>
      calls.filter((c) => c.method === 'GET' && c.url.pathname.endsWith(MAP_ID)).length;
    const before = mapLoads();
    await userEvent.click(screen.getByRole('button', { name: 'Ricarica' }));
    await waitFor(() => expect(mapLoads()).toBeGreaterThan(before));
    expect(await screen.findByRole('button', { name: 'Prova senza salvare' })).toBeInTheDocument();
  });

  it('lists the map issues of a 422', async () => {
    renderApp(
      `/m/${MARINA_ID}/maps/${MAP_ID}/import-berths`,
      routesFailingApply({
        status: 422,
        body: problem('map_invalid', 422, {
          issues: [
            { code: 'berth_overlap', severity: 'error', element_type: 'berth', refs: ['a', 'b'] },
          ],
        }),
      }),
    );
    await loadFile();
    await userEvent.click(screen.getByRole('button', { name: 'Prova senza salvare' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Importa' }));
    expect(await screen.findByText('La mappa risultante non è valida:')).toBeInTheDocument();
    expect(screen.getByText('berth_overlap (2 elementi)')).toBeInTheDocument();
  });

  it('is forbidden without harbor.configure', async () => {
    renderApp(`/m/${MARINA_ID}/maps/${MAP_ID}/import-berths`, [
      meRoute(makeMe({}, ['marina.read', 'map.view'])),
      { path: `${API}/maps/${MAP_ID}`, body: map },
    ]);
    expect(await screen.findByText('Non hai accesso a questa sezione.')).toBeInTheDocument();
  });
});
