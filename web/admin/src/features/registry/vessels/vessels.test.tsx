import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../../test/renderApp';
import { API, DOCUMENT_ID, VESSEL_ID, meWithRegistry, vessel } from '../test/fixtures';

vi.mock('../../maps/routes', () => ({ mapRoutes: () => [] }));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const vesselRoutes = (me = meWithRegistry()) => [
  meRoute(me),
  { path: `${API}/vessels/${VESSEL_ID}`, body: vessel },
  { path: `${API}/customers`, body: { items: [], next_cursor: null } },
];

describe('VesselsPage', () => {
  it('lists vessels with dimensions in metres and owners', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/vessels`, [
      meRoute(meWithRegistry()),
      { path: `${API}/customers`, body: { items: [], next_cursor: null } },
      {
        path: `${API}/vessels`,
        body: {
          items: [
            {
              id: VESSEL_ID,
              name: 'Libeccio',
              registration: 'GE1234',
              hull_type: 'monohull_sail',
              loa_cm: 1250,
              beam_cm: 410,
              effective_loa_cm: 1260,
              effective_beam_cm: 410,
              dimensions_source: 'measured',
              unit_class: 'imbarcazione',
              owners: [{ customer_id: 'c1', display_name: 'Mario Rossi' }],
              archived_at: null,
            },
          ],
          next_cursor: null,
        },
      },
    ]);
    // Table cells re-render when the owner filter options arrive: assert on the settled page.
    await waitFor(() => expect(screen.getByText('12,60 m × 4,10 m')).toBeInTheDocument());
    expect(await screen.findByText('Dimensioni misurate')).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'Mario Rossi' })).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Cerca'), 'GE12');
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('q') === 'GE12')).toBe(true),
    );
  });
});

describe('VesselPage', () => {
  it('shows declared dimensions and saves the form with If-Match', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/vessels/${VESSEL_ID}`, [
      ...vesselRoutes(),
      { method: 'PATCH', path: `${API}/vessels/${VESSEL_ID}`, body: vessel },
    ]);
    expect(await screen.findByRole('heading', { name: 'Libeccio' })).toBeInTheDocument();
    const loa = screen.getByLabelText('Lunghezza fuori tutto');
    expect(loa).toHaveValue('12,50');
    await userEvent.clear(loa);
    await userEvent.type(loa, '12,8');
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.headers.get('If-Match')).toBe('"5"');
    expect(patch.body).toMatchObject({ loa_cm: 1280, beam_cm: 410, power_required_a: 16 });
  });

  it('refuses owner shares above 100 % and saves valid parties with If-Match', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/vessels/${VESSEL_ID}`, [
      ...vesselRoutes(),
      { method: 'PUT', path: `${API}/vessels/${VESSEL_ID}/parties`, body: vessel },
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: /Proprietari e referenti/ }));
    const rows = await screen.findAllByTestId('party-row');
    expect(rows).toHaveLength(2);
    expect(screen.getByText('Quote dei proprietari: 100%')).toBeInTheDocument();
    const share = within(rows[0]!).getByLabelText('Quota');
    await userEvent.clear(share);
    await userEvent.type(share, '70');
    await userEvent.click(screen.getByRole('button', { name: 'Salva proprietari e referenti' }));
    expect(
      await screen.findByText('Le quote dei proprietari superano il 100%.'),
    ).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'PUT')).toBe(false);

    await userEvent.clear(share);
    await userEvent.type(share, '60');
    await userEvent.click(screen.getByRole('button', { name: 'Salva proprietari e referenti' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.headers.get('If-Match')).toBe('"5"');
    expect(put.body).toEqual({
      parties: [
        { customer_id: vessel.parties[0]!.customer_id, role: 'owner', share_millis: 600 },
        { customer_id: vessel.parties[1]!.customer_id, role: 'owner', share_millis: 400 },
      ],
    });
  });

  it('lets a dock user record measurements but not edit the vessel', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/vessels/${VESSEL_ID}`, [
      ...vesselRoutes(makeMe({}, ['marina.read', 'customers.read', 'presence.write'])),
      { method: 'POST', path: `${API}/vessels/${VESSEL_ID}/measurements`, body: vessel },
    ]);
    expect(await screen.findByRole('heading', { name: 'Libeccio' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Salva' })).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Archivia' })).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Registra misure' }));
    const dialog = await screen.findByRole('dialog');
    const loa = within(dialog).getByLabelText('Lunghezza fuori tutto');
    await userEvent.clear(loa);
    await userEvent.type(loa, '12,62');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva' }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.pathname.endsWith('/measurements'))).toBe(true),
    );
    expect(calls.find((c) => c.url.pathname.endsWith('/measurements'))!.body).toEqual({
      loa_cm: 1262,
      beam_cm: 410,
      draft_cm: 190,
    });
  });

  it('opens a document file through a signed link', async () => {
    const tab = { opener: {}, location: { href: '' }, close: vi.fn() };
    const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window);
    const signed = `${API}/documents/${DOCUMENT_ID}/file?exp=1&sig=abc`;
    const { calls } = renderApp(`/m/${MARINA_ID}/vessels/${VESSEL_ID}`, [
      ...vesselRoutes(),
      {
        method: 'POST',
        path: `${API}/documents/${DOCUMENT_ID}/file-url`,
        body: { url: signed, expires_at: '2026-11-06T10:05:00Z' },
      },
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: /Documenti/ }));
    expect(await screen.findByText('Polizza assicurativa')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'polizza.pdf' }));
    await waitFor(() => expect(tab.location.href).toBe(signed));
    expect(open).toHaveBeenCalledWith('', '_blank');
    expect(tab.opener).toBeNull();
    expect(calls.some((c) => c.url.pathname.endsWith('/file-url'))).toBe(true);
  });

  it('uploads a document file as a raw PUT with the CSRF header', async () => {
    const upload = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({ ...vessel.documents[0], has_file: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    );
    vi.stubGlobal('fetch', upload);
    document.cookie = 'porto_csrf=csrf-cookie';
    const { container } = renderApp(`/m/${MARINA_ID}/vessels/${VESSEL_ID}`, vesselRoutes());
    await userEvent.click(await screen.findByRole('tab', { name: /Documenti/ }));
    await userEvent.click(await screen.findByRole('button', { name: 'Sostituisci file' }));
    const input = container.ownerDocument.querySelector<HTMLInputElement>(
      'input[type=file][aria-hidden=true]',
    )!;
    const file = new File(['%PDF-1.4 test'], 'polizza nuova è.pdf', { type: 'application/pdf' });
    await userEvent.upload(input, file);
    await waitFor(() => expect(upload).toHaveBeenCalled());
    const [url, init] = upload.mock.calls[0]!;
    expect(String(url)).toBe(`${API}/documents/${DOCUMENT_ID}/file`);
    expect(init?.method).toBe('PUT');
    const headers = new Headers(init?.headers);
    expect(headers.get('Content-Type')).toBe('application/pdf');
    expect(headers.get('X-File-Name')).toBe('polizza nuova _.pdf');
    expect(headers.get('X-CSRF-Token')).toBe('csrf-cookie');
  });

  it('refuses a file of an unsupported type before uploading', async () => {
    const upload = vi.fn();
    vi.stubGlobal('fetch', upload);
    const { container } = renderApp(`/m/${MARINA_ID}/vessels/${VESSEL_ID}`, vesselRoutes());
    await userEvent.click(await screen.findByRole('tab', { name: /Documenti/ }));
    await userEvent.click(await screen.findByRole('button', { name: 'Sostituisci file' }));
    const input = container.ownerDocument.querySelector<HTMLInputElement>(
      'input[type=file][aria-hidden=true]',
    )!;
    await userEvent.upload(input, new File(['x'], 'nota.txt', { type: 'text/plain' }), {
      applyAccept: false,
    });
    expect(
      await screen.findByText('Formato non supportato: usa PDF, PNG o JPEG.'),
    ).toBeInTheDocument();
    expect(upload).not.toHaveBeenCalled();
  });
});
