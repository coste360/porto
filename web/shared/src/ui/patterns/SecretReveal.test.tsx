import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { renderWithProviders } from '../../testing';
import { SecretReveal } from './SecretReveal';

describe('SecretReveal', () => {
  it('shows the secret once with a warning and a copy button', async () => {
    const onDone = vi.fn();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    renderWithProviders(
      <SecretReveal secret="po_live_abc_secret" label="Chiave API" onDone={onDone} />,
    );

    expect(screen.getByLabelText('Chiave API')).toHaveValue('po_live_abc_secret');
    expect(screen.getByText(/non verrà più mostrato/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /Copia/ }));
    expect(writeText).toHaveBeenCalledWith('po_live_abc_secret');
    expect(await screen.findByRole('button', { name: /Copiato/ })).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Ho copiato il valore' }));
    expect(onDone).toHaveBeenCalled();
  });
});
