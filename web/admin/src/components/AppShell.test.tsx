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
    expect(screen.getByRole('button', { name: 'Mappa del porto' })).toBeDisabled();
  });
});
