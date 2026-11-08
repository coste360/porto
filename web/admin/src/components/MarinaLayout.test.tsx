import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type * as Offline from '@porto/shared/offline';

import { MARINA_ID, ORG_ID, makeMe, meRoute, renderApp } from '../test/renderApp';

const OTHER = '0190a000-0000-7000-8000-000000000002';

// The queue of the test: unsent ops in MARINA_ID.
vi.mock('@porto/shared/offline', async (importOriginal) => {
  const real = await importOriginal<typeof Offline>();
  return {
    ...real,
    marinaBlockingEntry: async (_user: string, target: string) =>
      target === MARINA_ID ? null : MARINA_ID,
  };
});

describe('marina route guard (D9)', () => {
  it('a deep link into another marina with unsent ops here goes back, with the reason', async () => {
    const me = makeMe();
    me.marinas.push({ ...me.marinas[0]!, id: OTHER, organization_id: ORG_ID, name: 'Altro porto' });
    const { router } = renderApp(`/m/${OTHER}`, [meRoute(me)]);
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}`));
    expect(
      await screen.findByText('Invia o scarta le operazioni in coda prima di cambiare porto.'),
    ).toBeInTheDocument();
  });
});
