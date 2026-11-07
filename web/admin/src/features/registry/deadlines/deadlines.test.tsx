import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../../test/renderApp';
import { API, deadline, meWithRegistry } from '../test/fixtures';

vi.mock('../../maps/routes', () => ({ mapRoutes: () => [] }));

const overdue = deadline({
  id: '0190a000-0000-7000-8000-00000000e002',
  urgency: 'overdue',
  due_on: '2026-10-01',
  kind: 'identity_document',
  document_type: 'identity_card',
  vessel: null,
});

describe('DeadlinesPage', () => {
  it('groups open deadlines by urgency and marks one done with If-Match', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/deadlines`, [
      meRoute(meWithRegistry()),
      { path: `${API}/deadlines`, body: { items: [overdue, deadline()], next_cursor: null } },
      {
        method: 'PATCH',
        path: `${API}/deadlines/${deadline().id}`,
        body: { ...deadline(), status: 'done' },
      },
    ]);
    const overdueSection = await screen.findByRole('region', { name: 'Scadute (1)' });
    expect(within(overdueSection).getByText('01/10/2026')).toBeInTheDocument();
    expect(within(overdueSection).getByText("Carta d'identità")).toBeInTheDocument();
    const dueSoon = screen.getByRole('region', { name: 'In scadenza (1)' });
    expect(within(dueSoon).getByRole('link', { name: 'Libeccio' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'Più avanti (0)' })).toBeInTheDocument();
    expect(
      calls.find((c) => c.url.pathname === `${API}/deadlines`)?.url.searchParams.get('status'),
    ).toBe('open');

    await userEvent.click(within(dueSoon).getByRole('button', { name: 'Gestita' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.body).toEqual({ status: 'done' });
    expect(patch.headers.get('If-Match')).toBe('"2"');
  });

  it('hides the actions without customers.write and the policies without marina.configure', async () => {
    renderApp(`/m/${MARINA_ID}/deadlines`, [
      meRoute(makeMe({}, ['marina.read', 'customers.read'])),
      { path: `${API}/deadlines`, body: { items: [deadline()], next_cursor: null } },
    ]);
    expect(await screen.findByRole('region', { name: 'In scadenza (1)' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Gestita' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Anticipi di avviso' })).toBeNull();
  });

  it('edits the lead days of each kind', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/deadlines`, [
      meRoute(meWithRegistry()),
      { path: `${API}/deadlines`, body: { items: [], next_cursor: null } },
      {
        path: `${API}/deadline-policies`,
        body: {
          items: [
            { kind: 'insurance', lead_days: [60, 15] },
            { kind: 'identity_document', lead_days: [30] },
          ],
        },
      },
      { method: 'PUT', path: `${API}/deadline-policies`, body: { items: [] } },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Anticipi di avviso' }));
    const dialog = await screen.findByRole('dialog');
    const insurance = await within(dialog).findByLabelText('Assicurazione');
    expect(insurance).toHaveValue('60, 15');
    await userEvent.clear(insurance);
    await userEvent.type(insurance, '90, x');
    expect(within(dialog).getByText(/Usa numeri interi/)).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Salva' })).toBeDisabled();
    await userEvent.clear(insurance);
    await userEvent.type(insurance, '15 90');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    expect(calls.find((c) => c.method === 'PUT')!.body).toEqual({
      items: [
        { kind: 'insurance', lead_days: [90, 15], channels: ['panel'] },
        { kind: 'identity_document', lead_days: [30], channels: ['panel'] },
      ],
    });
  });
});

describe('Dashboard deadlines card', () => {
  it('counts open deadlines by urgency', async () => {
    renderApp(`/m/${MARINA_ID}`, [
      meRoute(meWithRegistry()),
      {
        path: (url) =>
          url.pathname === `${API}/deadlines` && url.searchParams.get('urgency') === 'overdue',
        body: { items: [overdue, overdue], next_cursor: null },
      },
      {
        path: (url) =>
          url.pathname === `${API}/deadlines` && url.searchParams.get('urgency') === 'due_soon',
        body: { items: [deadline()], next_cursor: 'more' },
      },
      {
        path: (url) => url.pathname === `${API}/deadlines`,
        body: { items: [], next_cursor: null },
      },
    ]);
    expect(await screen.findByText('2')).toBeInTheDocument();
    expect(screen.getByTestId('deadlines-overdue')).toHaveTextContent('2');
    await waitFor(() => expect(screen.getByTestId('deadlines-due_soon')).toHaveTextContent('1+'));
    await waitFor(() => expect(screen.getByTestId('deadlines-later')).toHaveTextContent('0'));
    expect(screen.getByRole('link', { name: 'Vedi tutte' })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/deadlines`,
    );
  });

  it('is not shown without customers.read', async () => {
    renderApp(`/m/${MARINA_ID}`, [meRoute(makeMe())]);
    expect(await screen.findByText(/La panoramica operativa/)).toBeInTheDocument();
    expect(screen.queryByText('Vedi tutte')).toBeNull();
  });
});
