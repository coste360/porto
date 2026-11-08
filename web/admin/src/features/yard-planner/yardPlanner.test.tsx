import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { Planner } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, baseRoutes } from '../desk/test/fixtures';

const PERMS = ['marina.read', 'yard.read', 'yard.manage', 'map.view'] as const;

function planner(from: string): Planner {
  const days = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(`${from}T00:00:00Z`);
    d.setUTCDate(d.getUTCDate() + i);
    return d.toISOString().slice(0, 10);
  });
  return {
    from: days[0]!,
    to: days[6]!,
    today: days[2]!,
    rows: [
      {
        employee_id: 'e1',
        code: 'P1',
        display_name: 'Paolo Neri',
        department: '',
        qualification_ids: ['paint'],
        days: days.map((date, i) => ({
          date,
          capacity_milli: i < 5 ? 8000 : 0,
          assigned_milli: i === 0 ? 6000 : 0,
          overload: false,
          load_bp: i < 5 ? (i === 0 ? 7500 : 0) : null,
        })),
      },
    ],
    assignments: [],
    tasks: [
      {
        task_id: 't1',
        work_order_id: 'o1',
        work_order_number: 'C2027-000001',
        title: 'Carteggiatura',
        qualification_id: 'mech',
        estimated_hours_milli: 4000,
        remaining_milli: 4000,
        assigned_milli: 0,
        progress_bp: 0,
        depends_on: [],
        version: 3,
      },
    ],
  };
}

const routes = (
  extra: { method?: string; path: string; status?: number; body?: unknown }[] = [],
) => [
  ...baseRoutes(makeMe({}, [...PERMS])),
  ...extra,
  { path: `${API}/yard/qualifications`, body: { items: [] } },
  { path: `${API}/yard/planner`, body: planner('2026-11-02') },
];

describe('yard planner page', () => {
  it('assigns a tray task with the keyboard, confirming the qualification override', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/yard/planner`,
      routes([
        {
          method: 'PUT',
          path: `${API}/yard/planner/assignments`,
          body: { assignments: [], tasks: [{ task_id: 't1', version: 4 }] },
        },
      ]),
    );
    const task = await screen.findByTestId('tray-task');
    expect(task).toHaveTextContent('Carteggiatura');
    await userEvent.click(task);
    const grid = screen.getByTestId('planner-grid');
    grid.focus();
    fireEvent.keyDown(grid, { key: 'ArrowRight' });
    fireEvent.keyDown(grid, { key: 'Enter' });
    const dialog = await screen.findByTestId('assign-dialog');
    const assign = within(dialog).getByRole('button', { name: 'Assegna' });
    expect(assign).toBeDisabled();
    await userEvent.click(within(dialog).getByLabelText(/non ha la qualifica/));
    await userEvent.click(assign);
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.body).toMatchObject({
      force_qualification: true,
      allow_overload: false,
      changes: [
        {
          task_id: 't1',
          employee_id: 'e1',
          work_date: '2026-11-03',
          hours_milli: 4000,
          task_version: 3,
        },
      ],
    });
  });

  it('shows capacity_exceeded from the server', async () => {
    renderApp(
      `/m/${MARINA_ID}/yard/planner`,
      routes([
        {
          method: 'PUT',
          path: `${API}/yard/planner/assignments`,
          status: 409,
          body: { type: 'capacity_exceeded', title: 'x', status: 409, details: {} },
        },
      ]),
    );
    await userEvent.click(await screen.findByTestId('tray-task'));
    const grid = screen.getByTestId('planner-grid');
    fireEvent.keyDown(grid, { key: 'ArrowRight' });
    fireEvent.keyDown(grid, { key: 'Enter' });
    const dialog = await screen.findByTestId('assign-dialog');
    await userEvent.click(within(dialog).getByLabelText(/non ha la qualifica/));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Assegna' }));
    // The qualification warning is an alert too: look for the server problem by its message.
    expect(
      await within(dialog).findByText(/supera la capacità del dipendente/),
    ).toBeInTheDocument();
  });
});
