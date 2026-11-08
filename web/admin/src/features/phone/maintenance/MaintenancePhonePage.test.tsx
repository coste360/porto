import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, B1, baseRoutes } from '../../desk/test/fixtures';

const task = {
  id: 't1',
  title: 'Prova differenziale',
  source: 'plan',
  priority: 'urgent',
  status: 'open',
  out_of_service: false,
  overdue: true,
  due_on: '2026-11-01',
  element: { element_type: 'berth', element_id: B1, code: 'A01' },
  checklist: [{ key: 'rcd', label: 'Test differenziale' }],
  checklist_results: [],
  allocation_ids: [],
  files: [],
  version: 1,
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
};

describe('MaintenancePhonePage', () => {
  it('shows the worklist and opens a task with its checklist and outcome', async () => {
    renderApp(`/m/${MARINA_ID}/maintenance-phone`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'maintenance.report', 'map.view'])),
      { path: `${API}/maintenance/worklist`, body: { date: '2026-11-08', items: [task] } },
    ]);
    const item = await screen.findByTestId('worklist-task');
    expect(item).toHaveTextContent('Prova differenziale');
    expect(item).toHaveTextContent('Scaduto');
    expect(item).toHaveTextContent('Urgente');
    await userEvent.click(item);
    const form = await screen.findByTestId('outcome-form');
    expect(within(form).getByLabelText('Test differenziale')).toBeInTheDocument();
    expect(within(form).getByRole('button', { name: 'Registra esito' })).toBeEnabled();
    await userEvent.click(within(form).getByRole('button', { name: 'Indietro' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Segnala' }));
    const report = await screen.findByTestId('report-form');
    expect(within(report).getByRole('button', { name: 'Invia segnalazione' })).toBeDisabled();
  });
});
