import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import type { MaintenanceTask } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, B1, baseRoutes } from '../desk/test/fixtures';
import { conflictsOf, periodicityText, sortTasks } from './lib';

const TASK_ID = '0190a000-0000-7000-8000-00000000f001';
const PERMS = ['marina.read', 'maintenance.manage', 'maintenance.report', 'map.view'] as const;

const task = (over: Partial<MaintenanceTask> = {}): MaintenanceTask => ({
  id: TASK_ID,
  title: 'Sostituire cima',
  source: 'office',
  priority: 'high',
  status: 'open',
  out_of_service: true,
  overdue: false,
  due_on: '2026-11-20',
  element: { element_type: 'berth', element_id: B1, code: 'A01' },
  checklist: [
    { key: 'cima', label: 'Cima' },
    { key: 'grillo', label: 'Grillo' },
  ],
  checklist_results: [],
  allocation_ids: [],
  files: [],
  version: 3,
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
  ...over,
});

const routes = (
  extra: { method?: string; path: string; status?: number; body?: unknown }[] = [],
) => [
  ...baseRoutes(makeMe({}, [...PERMS])),
  ...extra,
  { path: `${API}/maintenance/tasks/${TASK_ID}`, body: task() },
  { path: `${API}/maintenance/tasks`, body: { items: [task()], next_cursor: null } },
  { path: `${API}/equipment`, body: { items: [], next_cursor: null } },
];

describe('maintenance pages', () => {
  beforeAll(() => {
    // cmdk scrolls the active option into view; jsdom has no layout.
    Element.prototype.scrollIntoView = vi.fn();
  });

  it('lists the tasks with filters and links to the detail', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/maintenance`, routes());
    const row = await screen.findByTestId('maintenance-task-row');
    expect(row).toHaveTextContent('Sostituire cima');
    expect(row).toHaveTextContent('Posto A01');
    expect(row).toHaveTextContent('Alta');
    await userEvent.click(screen.getByLabelText('Solo scaduti'));
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('overdue_only') === 'true')).toBe(true),
    );
  });

  it('lists the vessels holding the nights of a refused schedule, with their alternatives', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/maintenance/tasks/${TASK_ID}`,
      routes([
        {
          method: 'POST',
          path: `${API}/maintenance/tasks/${TASK_ID}/schedule`,
          status: 409,
          body: {
            type: 'maintenance_conflicts',
            title: 'conflicts',
            status: 409,
            details: {
              conflicts: [
                {
                  allocation_id: 'al-1',
                  resource_type: 'berth',
                  resource_id: B1,
                  from: '2026-11-20',
                  to: '2026-11-21',
                  alternatives: [
                    {
                      key: 's1',
                      score: 1,
                      fit: { ok: true, blocking: [], warnings: [] },
                      segments: [
                        {
                          resource_type: 'berth',
                          resource_id: 'b',
                          code: 'A07',
                          map_id: 'm',
                          first_night: '2026-11-20',
                          last_night: '2026-11-21',
                        },
                      ],
                    },
                  ],
                },
              ],
            },
          },
        },
      ]),
    );
    await screen.findByRole('heading', { name: 'Sostituire cima' });
    await userEvent.click(screen.getByRole('button', { name: 'Pianifica' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Pianifica' }));
    const list = await screen.findByTestId('maintenance-conflicts');
    expect(list).toHaveTextContent('1 barca occupa le notti');
    expect(list).toHaveTextContent('Occupato dal 20/11/2026 al 21/11/2026');
    expect(list).toHaveTextContent('Posti liberi: A07');
    const call = calls.find((c) => c.url.pathname.endsWith('/schedule'))!;
    expect(call.headers.get('If-Match')).toBe('"3"');
    expect(call.body).toEqual({
      from_night: '2026-11-20',
      to_night: '2026-11-20',
      out_of_service: true,
    });
  });

  it('builds a plan with targets and a checklist', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/maintenance/plans`,
      routes([
        { path: `${API}/maintenance/plans`, body: { items: [], next_cursor: null } },
        { path: `${API}/utility-meters`, body: { items: [], next_cursor: null } },
        { method: 'POST', path: `${API}/maintenance/plans`, status: 201, body: {} },
      ]),
    );
    await userEvent.click(await screen.findByRole('button', { name: 'Nuovo piano' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Nome'), 'Ispezione');
    await userEvent.click(within(dialog).getByRole('combobox', { name: 'Elemento' }));
    await userEvent.click(await screen.findByRole('option', { name: 'A01' }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Aggiungi elemento' }));
    expect(within(dialog).getByTestId('plan-targets')).toHaveTextContent('Posto A01');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Aggiungi voce' }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Aggiungi voce' }));
    const items = within(dialog).getAllByTestId('checklist-row');
    await userEvent.type(within(items[0]!).getByRole('textbox'), 'Catena');
    await userEvent.type(within(items[1]!).getByRole('textbox'), 'Grillo');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva piano' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const body = calls.find((c) => c.method === 'POST')!.body as Record<string, unknown>;
    expect(body.targets).toEqual([{ element_type: 'berth', element_id: B1 }]);
    expect(body.checklist).toEqual([
      { key: 'k1', label: 'Catena' },
      { key: 'k2', label: 'Grillo' },
    ]);
    expect(body.periodicity).toEqual({ kind: 'months', every: 6 });
  });
});

describe('maintenance helpers', () => {
  it('puts overdue tasks first, then by due date, undated last', () => {
    const list = sortTasks([
      task({ id: 'a', due_on: undefined, title: 'A' }),
      task({ id: 'b', due_on: '2026-12-01', title: 'B' }),
      task({ id: 'c', due_on: '2026-12-05', overdue: true, title: 'C' }),
      task({ id: 'd', due_on: '2026-11-30', title: 'D' }),
    ]);
    expect(list.map((x) => x.id)).toEqual(['c', 'd', 'b', 'a']);
  });

  it('describes the periodicity and reads the conflicts only from maintenance_conflicts', () => {
    const t = (k: string, p?: Record<string, unknown>) => `${k}${p ? JSON.stringify(p) : ''}`;
    expect(periodicityText(t, { kind: 'months', every: 6 })).toBe(
      'maintenance:period.months{"count":6}',
    );
    expect(periodicityText(t, { kind: 'cycles', every: 250_000 })).toBe(
      'maintenance:period.cycles{"count":250}',
    );
    expect(conflictsOf(new Error('x'))).toBeNull();
  });
});
