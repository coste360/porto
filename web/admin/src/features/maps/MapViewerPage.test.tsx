import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { makeMapDocument } from './testFixtures';

const mapPath = `/api/admin/v1/marinas/${MARINA_ID}/maps/map-1`;

describe('MapViewerPage', () => {
  it('shows the map read-only with the list by structure, without edit for viewers', async () => {
    renderApp(`/m/${MARINA_ID}/maps/map-1`, [
      meRoute(makeMe({}, ['marina.read', 'map.view'])),
      { path: mapPath, body: makeMapDocument() },
    ]);
    expect(await screen.findByRole('heading', { name: 'Porto' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Posto A-1' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /Modifica nell'editor/ })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /Pontile A.*Posti/ }));
    expect(await screen.findByText('A-3')).toBeInTheDocument();
    // 500 x 300 m map: on narrow screens the canvas takes the map's proportions, fixed height from lg.
    expect(screen.getByTestId('map-canvas')).toHaveClass(
      'aspect-video',
      'lg:h-160',
      'lg:aspect-auto',
    );
  });

  it('links to the editor for harbor.configure', async () => {
    renderApp(`/m/${MARINA_ID}/maps/map-1`, [
      meRoute(makeMe({}, ['marina.read', 'map.view', 'harbor.configure'])),
      { path: mapPath, body: makeMapDocument() },
    ]);
    const edit = await screen.findByRole('link', { name: /Modifica nell'editor/ });
    expect(edit).toHaveAttribute('href', `/admin/m/${MARINA_ID}/maps/map-1/edit`);
  });

  it('renders the print page with the scale', async () => {
    renderApp(`/m/${MARINA_ID}/maps/map-1/print`, [
      meRoute(makeMe({}, ['marina.read', 'map.view'])),
      { path: mapPath, body: makeMapDocument() },
    ]);
    expect(await screen.findByTestId('print-scale')).toHaveTextContent(/^Scala 1:\d+$/);
    expect(screen.getByRole('img', { name: 'Foglio di stampa della mappa' })).toBeInTheDocument();
  });
});
