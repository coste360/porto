import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { setOfflineSession } from '../../offline/index';
import { dbPut } from '../../offline/db';
import { reloadOps } from '../../offline/queue';
import { M, USER, op, resetOffline, setOnline } from '../../offline/testkit';
import { renderWithProviders } from '../../testing';
import { OfflineIndicator } from './OfflineIndicator';
import { OfflineReason } from './OfflineReason';

async function seed(...ops: ReturnType<typeof op>[]) {
  for (const o of ops) await dbPut('ops', o);
  await act(async () => {
    await reloadOps();
  });
}

// jsdom lacks pointer capture, used by the vaul drawer.
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
}

describe('OfflineIndicator and QueueSheet', () => {
  beforeEach(() => {
    resetOffline();
    setOfflineSession(USER);
  });

  it('shows the status, the pending count and the conflicts', async () => {
    setOnline(false);
    renderWithProviders(<OfflineIndicator marinaId={M} />);
    await seed(
      op('1', { label: { key: 'offline:queue.label.arrived', params: { vessel: 'Libeccio' } } }),
      op('2', { state: 'retrying' }),
      op('3', { state: 'conflict', problem: { status: 409, type: 'allocation_changed' } }),
      op('4', { state: 'done' }),
    );
    const btn = screen.getByTestId('offline-indicator');
    expect(btn).toHaveAttribute('data-online', 'false');
    expect(btn).toHaveAttribute('data-pending', '2');
    expect(btn).toHaveAttribute('data-conflicts', '1');
    expect(screen.getByTestId('offline-pending')).toHaveTextContent('2 operazioni in coda');
    expect(screen.getByTestId('offline-conflicts')).toHaveTextContent('1 conflitto');
  });

  it('a conflict shows the problem and the new berth; Scarta and Riprova are the only actions', async () => {
    const user = userEvent.setup();
    renderWithProviders(<OfflineIndicator marinaId={M} />);
    await seed(
      op('1', {
        state: 'conflict',
        label: { key: 'offline:queue.label.arrived', params: { vessel: 'Libeccio' } },
        problem: {
          status: 409,
          type: 'allocation_changed',
          detail: { current_resource_id: 'b2', current_resource_code: 'B-07' },
        },
      }),
      op('2', { userId: 'other-user' }),
    );
    await user.click(screen.getByTestId('offline-indicator'));
    const sheet = await screen.findByTestId('queue-sheet');
    expect(sheet).toHaveTextContent('Arrivo di Libeccio');
    expect(screen.getByTestId('queue-op-problem')).toHaveTextContent(
      "L'ormeggio della barca è cambiato",
    );
    expect(screen.getByTestId('queue-op-problem')).toHaveTextContent('B-07');
    expect(sheet).toHaveTextContent(
      'Ci sono operazioni in coda di un altro utente: possono solo essere scartate.',
    );
    const discards = screen.getAllByRole('button', { name: 'Scarta' });
    expect(discards).toHaveLength(2);
    expect(screen.getAllByRole('button', { name: 'Riprova come nuova operazione' })).toHaveLength(
      1,
    );
    await user.click(discards[0]!);
    await waitFor(() => expect(screen.getAllByTestId('queue-op')).toHaveLength(1));
  });

  it('OfflineReason shows the reason inline', () => {
    renderWithProviders(<OfflineReason reason="allocation" />);
    expect(screen.getByTestId('offline-reason')).toHaveTextContent(
      'Non disponibile offline: richiede la garanzia di disponibilità del server.',
    );
  });
});
