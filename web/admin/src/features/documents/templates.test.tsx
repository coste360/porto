import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Schemas } from '@porto/shared/api';
import { problem } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { buildAudience, EMPTY_AUDIENCE } from './lib/audience';

vi.mock('../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

const API = `/api/admin/v1/marinas/${MARINA_ID}`;
const TPL = '0190a000-0000-7000-8000-0000000d0001';

const me = makeMe({}, [
  'marina.read',
  'templates.configure',
  'contracts.read',
  'communications.send',
  'customers.read',
]);

const template: Schemas['Template'] = {
  id: TPL,
  kind: 'contract_annual',
  code: 'ANN',
  name: 'Contratto annuale',
  is_default: true,
  locales: [{ locale: 'it', version: 1 }],
  created_at: '2026-11-01T10:00:00Z',
  version: 1,
};

const draft: Schemas['TemplateVersion'] = {
  id: 'v1',
  template_id: TPL,
  version: 1,
  locale: 'it',
  body_md: 'Gentile ',
  header_md: '',
  footer_md: '',
  page: {},
  status: 'draft',
  created_at: '2026-11-01T10:00:00Z',
};

const kinds = {
  items: [
    {
      kind: 'contract_annual',
      publishable: true,
      fields: [
        {
          path: 'customer',
          type: 'object',
          list: false,
          description_key: 'fields.party.display_name',
        },
        {
          path: 'customer.name',
          type: 'text',
          list: false,
          description_key: 'fields.party.display_name',
        },
        {
          path: 'contract',
          type: 'object',
          list: false,
          description_key: 'fields.contract.code',
        },
        {
          path: 'contract.total',
          type: 'cents',
          list: false,
          description_key: 'fields.contract.code',
        },
      ],
    },
  ],
};

const routes = (extra: Parameters<typeof renderApp>[1] = []) => [
  meRoute(me),
  {
    path: `${API}`,
    body: { id: MARINA_ID, name: 'Marina Prova', slug: 'prova', timezone: 'Europe/Rome' },
  },
  { path: `${API}/templates/${TPL}`, body: template },
  { path: `${API}/templates/${TPL}/versions`, body: { items: [draft] } },
  { path: `${API}/template-kinds`, body: kinds },
  ...extra,
];

let created: string[] = [];
beforeEach(() => {
  created = [];
  URL.createObjectURL = vi.fn(() => {
    const u = `blob:preview-${created.length + 1}`;
    created.push(u);
    return u;
  });
  URL.revokeObjectURL = vi.fn();
});
afterEach(() => vi.restoreAllMocks());

describe('templates editor', () => {
  it('inserts a field from the catalogue tree at the caret with its default filter', async () => {
    renderApp(`/m/${MARINA_ID}/templates/${TPL}`, routes());
    const area = await screen.findByRole('textbox', { name: /Testo del modello/ });
    expect(area).toHaveValue('Gentile ');
    await userEvent.click(screen.getByRole('button', { name: 'Apri customer' }));
    await userEvent.click(screen.getByRole('button', { name: 'name' }));
    expect(area).toHaveValue('Gentile {{customer.name}}');
    await userEvent.click(screen.getByRole('button', { name: 'Apri contract' }));
    await userEvent.click(screen.getByRole('button', { name: 'total' }));
    expect(area).toHaveValue('Gentile {{customer.name}}{{contract.total | money}}');
  });

  it('shows the template_invalid issues by line and marks the textarea', async () => {
    renderApp(
      `/m/${MARINA_ID}/templates/${TPL}`,
      routes([
        {
          method: 'POST',
          path: `${API}/templates/${TPL}/versions`,
          status: 422,
          body: {
            ...problem('template_invalid', 422),
            details: { issues: [{ line: 2, col: 3, code: 'unknown_path', path: 'customer.nam' }] },
          },
        },
      ]),
    );
    const area = await screen.findByRole('textbox', { name: /Testo del modello/ });
    fireEvent.change(area, { target: { value: 'Gentile\n  {{customer.nam}}' } });
    await userEvent.click(screen.getByRole('button', { name: 'Salva bozza' }));
    const issues = await screen.findByTestId('template-issues');
    expect(issues).toHaveTextContent('Testo, riga 2, colonna 3');
    expect(issues).toHaveTextContent('campo sconosciuto (customer.nam)');
    expect(area).toHaveAttribute('aria-invalid', 'true');
    const markers = screen.getByRole('list', { name: 'Righe con errori' });
    expect(within(markers).getByText(/^2:/)).toHaveAttribute('data-line', '2');
  });

  it('renders the preview blob in an iframe and revokes the previous URL', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/templates/${TPL}`,
      routes([{ method: 'POST', path: `${API}/templates/${TPL}/preview`, body: { pdf: true } }]),
    );
    await screen.findByRole('textbox', { name: /Testo del modello/ });
    await userEvent.click(screen.getByRole('button', { name: 'Anteprima PDF' }));
    const frame = await screen.findByTestId('template-preview');
    expect(frame).toHaveAttribute('src', 'blob:preview-1');
    const body = calls.find((c) => c.url.pathname.endsWith('/preview'))!.body;
    expect(body).toEqual({ locale: 'it', body_md: 'Gentile ', sample: 'demo' });
    await userEvent.click(screen.getByRole('button', { name: 'Anteprima PDF' }));
    await waitFor(() =>
      expect(screen.getByTestId('template-preview')).toHaveAttribute('src', 'blob:preview-2'),
    );
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-1');
  });
});

describe('communications audience builder', () => {
  it('builds the audience from the wizard criteria and refuses an empty one', async () => {
    renderApp(`/m/${MARINA_ID}/communications/new`, [
      meRoute(me),
      {
        path: `${API}`,
        body: { id: MARINA_ID, name: 'Marina Prova', slug: 'prova', timezone: 'Europe/Rome' },
      },
      { path: `${API}/berth-groups`, body: { items: [] } },
      { path: `${API}/templates`, body: { items: [] } },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Avanti' }));
    expect(await screen.findByText('Scegli almeno un criterio.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: "Titolari di diritti d'uso" }));
    await userEvent.click(screen.getByRole('checkbox', { name: 'Stagionale' }));
    await userEvent.click(screen.getByRole('button', { name: 'Avanti' }));
    expect(await screen.findByLabelText('Oggetto')).toBeInTheDocument();
    expect(buildAudience({ ...EMPTY_AUDIENCE, holders: true, terms: ['seasonal'] })).toEqual({
      ok: true,
      audience: { holders: true, terms: ['seasonal'] },
    });
  });
});
