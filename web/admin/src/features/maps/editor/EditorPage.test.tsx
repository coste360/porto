import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';

import type { GenerateResult, MapSaveResult } from '@porto/shared/api';
import { problem, type MockRoute } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../../test/renderApp';
import { berth, makeMapDocument } from '../testFixtures';
import { draftKey } from './model/draft';

const base = `/api/admin/v1/marinas/${MARINA_ID}`;
const mapPath = `${base}/maps/map-1`;
const owner = makeMe({}, ['marina.read', 'map.view', 'harbor.configure']);

function routes(extra: MockRoute[] = []): MockRoute[] {
  return [
    meRoute(owner),
    ...extra,
    { path: mapPath, body: makeMapDocument() },
    { path: `${base}/berth-groups`, body: { items: [] } },
    {
      path: `${base}/harbor-settings`,
      body: {
        version: 1,
        loa_tolerance_cm: 50,
        default_grid_cm: 50,
        price_class_rule: {
          basis: 'width',
          bands: [{ max_cm: 400, class: 'L12' }],
          fallback: 'L25',
        },
      },
    },
    { method: 'POST', path: `${mapPath}/validate`, body: { errors: [], warnings: [] } },
  ];
}

const saved = (): MapSaveResult => ({
  document: makeMapDocument({ version: 4 }),
  id_map: {},
  warnings: [],
});

async function openEditor(extra: MockRoute[] = []) {
  const r = renderApp(`/m/${MARINA_ID}/maps/map-1/edit`, routes(extra));
  await screen.findByRole('toolbar', { name: "Strumenti dell'editor" });
  return r;
}

async function selectBerth(code: string) {
  const el = screen.getByRole('button', { name: `Posto ${code}` });
  el.focus();
  await userEvent.keyboard('{Enter}');
}

// react-resizable-panels listens to pointerdown on the document in the capture phase and, with the zero-sized
// layout of jsdom, takes every pointer: the tests drive the editor with keyboard and fireEvent instead.
async function setWidth(metres: string) {
  const input = await screen.findByLabelText('Larghezza');
  input.focus();
  fireEvent.change(input, { target: { value: metres } });
  fireEvent.keyDown(input, { key: 'Enter' });
  input.blur();
}

function clickSave() {
  fireEvent.click(screen.getByRole('button', { name: 'Salva' }));
}

async function openMenu(name: string) {
  const trigger = screen.getByRole('button', { name });
  trigger.focus();
  await userEvent.keyboard('{Enter}');
}

afterEach(() => {
  localStorage.clear();
});

describe('EditorPage', () => {
  it('loads the document, edits a berth and saves with If-Match', async () => {
    const { calls } = await openEditor([{ method: 'PUT', path: mapPath, body: saved() }]);
    expect(screen.getByTestId('berth-count')).toHaveTextContent('Posti: 3');
    await selectBerth('A-1');
    expect(await screen.findByText('Elementi selezionati: 1')).toBeInTheDocument();
    await setWidth('5');
    expect(screen.getAllByText('Modifiche non salvate').length).toBeGreaterThan(0);
    clickSave();
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.headers.get('If-Match')).toBe('"3"');
    const body = put.body as { berths: { id: string; width_cm: number }[] };
    expect(body.berths.find((b) => b.id === 'b1')?.width_cm).toBe(500);
    expect(await screen.findByText('Mappa salvata.')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByText('Modifiche non salvate')).not.toBeInTheDocument(),
    );
  });

  it('shows the side panel tabs as one row of labelled icons', async () => {
    await openEditor();
    const list = screen.getByRole('tablist');
    expect(list).toHaveClass('grid-cols-6');
    const tabs = within(list).getAllByRole('tab');
    expect(tabs.map((tab) => tab.textContent)).toEqual([
      'Proprietà',
      'Generatore',
      'Sfondo',
      'Verifica',
      'Livelli',
      'Elenco',
    ]);
    for (const tab of tabs) expect(tab.querySelector('svg')).not.toBeNull();
  });

  it('refuses a berth side below the 2 cm minimum', async () => {
    await openEditor();
    await selectBerth('A-1');
    await setWidth('0,01');
    expect(screen.queryByText('Modifiche non salvate')).not.toBeInTheDocument();
    await setWidth('0,02');
    expect((await screen.findAllByText('Modifiche non salvate')).length).toBeGreaterThan(0);
  });

  it('undoes with Ctrl+Z and redoes with Ctrl+Y', async () => {
    await openEditor();
    await selectBerth('A-2');
    await setWidth('6');
    (document.activeElement as HTMLElement | null)?.blur();
    await userEvent.keyboard('{Control>}z{/Control}');
    await waitFor(() =>
      expect(screen.queryByText('Modifiche non salvate')).not.toBeInTheDocument(),
    );
    await userEvent.keyboard('{Control>}y{/Control}');
    expect((await screen.findAllByText('Modifiche non salvate')).length).toBeGreaterThan(0);
  });

  it('runs the live validation on the unsaved document', async () => {
    const { calls } = await openEditor();
    await waitFor(
      () => expect(calls.some((c) => c.url.pathname.endsWith('/validate'))).toBe(true),
      { timeout: 3000 },
    );
    const body = calls.find((c) => c.url.pathname.endsWith('/validate'))!.body as {
      berths: unknown[];
    };
    expect(body.berths).toHaveLength(3);
  });

  it('shows the 422 issues in the validation panel', async () => {
    await openEditor([
      {
        method: 'PUT',
        path: mapPath,
        status: 422,
        body: problem('map_invalid', 422, {
          issues: [
            { code: 'berth_overlap', severity: 'error', element_type: 'berth', refs: ['b1', 'b2'] },
          ],
        }),
      },
    ]);
    await selectBerth('A-1');
    await setWidth('9');
    clickSave();
    const panel = await screen.findByRole('region', { name: 'Verifica della mappa' });
    expect(within(panel).getByText('Posti sovrapposti')).toBeInTheDocument();
    expect(within(panel).getByRole('button', { name: 'A-1 / A-2' })).toBeInTheDocument();
  });

  it('opens the conflict dialog on 412 and reloads the server version', async () => {
    await openEditor([
      { method: 'PUT', path: mapPath, status: 412, body: problem('version_conflict', 412) },
    ]);
    await selectBerth('A-1');
    await setWidth('7');
    clickSave();
    const dialog = await screen.findByRole('alertdialog');
    expect(await within(dialog).findByTestId('conflict-details')).toHaveTextContent('versione 3');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Ricarica' }));
    await waitFor(() =>
      expect(screen.queryByText('Modifiche non salvate')).not.toBeInTheDocument(),
    );
  });

  it('previews a generator and inserts its elements as one undo step', async () => {
    const preview: GenerateResult = {
      elements: {
        structures: [],
        berths: [
          berth('x', { id: undefined, key: 'g1-b1', code: 'B-1', anchor: { x: 20_000, y: 1000 } }),
          berth('y', { id: undefined, key: 'g1-b2', code: 'B-2', anchor: { x: 20_000, y: 1400 } }),
        ],
        dynamic_areas: [],
        land_resources: [],
        pedestals: [],
        decorations: [],
      },
      report: { errors: [], warnings: [] },
      counts: {
        structures: 0,
        berths: 2,
        dynamic_areas: 0,
        land_resources: 0,
        pedestals: 0,
        decorations: 0,
      },
    };
    const { calls } = await openEditor([
      { method: 'POST', path: `${mapPath}/generate`, body: preview },
    ]);
    await openMenu('Generatori');
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Pontile con posti' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Scegli sulla mappa' }));
    const canvas = screen.getByTestId('editor-canvas');
    fireEvent.pointerDown(canvas, { button: 0, clientX: 400, clientY: 300, pointerId: 1 });
    expect(screen.getByTestId('gen-origin')).not.toHaveTextContent('Nessun punto');
    fireEvent.click(screen.getByRole('button', { name: 'Anteprima' }));
    expect(await screen.findByTestId('generator-preview')).toHaveTextContent('2 posti');
    const req = calls.find((c) => c.url.pathname.endsWith('/generate'))!.body as {
      generator: { type: string; key_prefix: string; origin: { x: number; y: number } };
      document: { berths: unknown[] };
    };
    expect(req.generator.type).toBe('pier');
    expect(req.generator.key_prefix).toMatch(/^g\d+-$/);
    expect(Number.isInteger(req.generator.origin.x)).toBe(true);
    expect(req.document.berths).toHaveLength(3);
    fireEvent.click(screen.getByRole('button', { name: 'Inserisci' }));
    expect(screen.getByTestId('berth-count')).toHaveTextContent('Posti: 5');
    act(() => {
      (document.activeElement as HTMLElement | null)?.blur();
    });
    await userEvent.keyboard('{Control>}z{/Control}');
    expect(screen.getByTestId('berth-count')).toHaveTextContent('Posti: 3');
  });

  it('creates a berth with the Berth tool and deletes it with Del', async () => {
    await openEditor();
    fireEvent.click(screen.getByRole('radio', { name: 'Posto (B)' }));
    fireEvent.pointerDown(screen.getByTestId('editor-canvas'), {
      button: 0,
      clientX: 600,
      clientY: 500,
      pointerId: 1,
    });
    expect(screen.getByTestId('berth-count')).toHaveTextContent('Posti: 4');
    act(() => {
      (document.activeElement as HTMLElement | null)?.blur();
    });
    await userEvent.keyboard('{Delete}');
    expect(screen.getByTestId('berth-count')).toHaveTextContent('Posti: 3');
  });

  it('offers to restore a local draft', async () => {
    const doc = makeMapDocument();
    const { id: _id, version: _v, updated_at: _u, ...body } = doc;
    localStorage.setItem(
      draftKey('map-1', 3),
      JSON.stringify({
        savedAt: '2026-11-06T09:00:00Z',
        body: { ...body, berths: body.berths.slice(0, 1) },
      }),
    );
    renderApp(`/m/${MARINA_ID}/maps/map-1/edit`, routes());
    const dialog = await screen.findByRole('alertdialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Riprendi la bozza' }));
    expect(screen.getByTestId('berth-count')).toHaveTextContent('Posti: 1');
    expect(screen.getAllByText('Modifiche non salvate').length).toBeGreaterThan(0);
  });

  it('is forbidden without harbor.configure', async () => {
    renderApp(`/m/${MARINA_ID}/maps/map-1/edit`, [
      meRoute(makeMe({}, ['marina.read', 'map.view'])),
      { path: mapPath, body: makeMapDocument() },
    ]);
    expect(await screen.findByText(/Non hai accesso/)).toBeInTheDocument();
  });
});
