import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { renderWithProviders } from '../../testing';
import { LocalModeNotice } from './LocalModeNotice';

describe('LocalModeNotice', () => {
  it('renders nothing outside local mode', () => {
    renderWithProviders(<LocalModeNotice localMode={false} feature="publicPage" />);
    expect(screen.queryByTestId('local-mode-notice')).not.toBeInTheDocument();
  });

  it('explains the unavailable feature in local mode, with the action', () => {
    renderWithProviders(
      <LocalModeNotice
        localMode
        feature="remoteSignature"
        action={<a href="/admin/desktop/online">guida</a>}
      />,
    );
    const notice = screen.getByTestId('local-mode-notice');
    expect(notice).toHaveTextContent('Non disponibile nella versione desktop');
    expect(notice).toHaveTextContent(/firma a distanza/);
    expect(screen.getByRole('link', { name: 'guida' })).toBeInTheDocument();
  });

  it('is translated in English', () => {
    renderWithProviders(<LocalModeNotice localMode feature="onlinePayments" />, { locale: 'en' });
    expect(screen.getByTestId('local-mode-notice')).toHaveTextContent(
      'Not available in the desktop edition',
    );
  });
});
