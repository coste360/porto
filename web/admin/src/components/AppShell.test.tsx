import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../test/renderApp';

describe('AppShell', () => {
  it('shows only the allowed sections and the future modules disabled', async () => {
    renderApp(`/m/${MARINA_ID}`, [meRoute(makeMe({}, ['marina.read', 'devices.pair_self']))]);
    expect(await screen.findByRole('link', { name: 'Panoramica' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Dispositivi' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Team' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Registro attività' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Mappe' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Contratti e transiti' })).toBeDisabled();
  });

  it('shows the M1 sections by permission', async () => {
    renderApp(`/m/${MARINA_ID}`, [
      meRoute(makeMe({}, ['marina.read', 'map.view', 'customers.read'])),
    ]);
    expect(await screen.findByRole('link', { name: 'Mappe' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Clienti' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Imbarcazioni' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Scadenze' })).toBeInTheDocument();
    expect(
      screen.queryByRole('link', { name: 'Impostazioni della mappa' }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Mappa del porto' })).not.toBeInTheDocument();
  });
});
