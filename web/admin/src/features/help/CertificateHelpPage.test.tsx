import { screen } from '@testing-library/react';

import { renderWithProviders } from '@porto/shared/testing';

import { CertificateHelpPage } from './CertificateHelpPage';
import { fingerprintFromSearch } from './certificate';

describe('CertificateHelpPage', () => {
  it('links the CA certificate and the iOS profile on the local-network listener', () => {
    renderWithProviders(<CertificateHelpPage />);
    expect(screen.getByTestId('ca-download')).toHaveAttribute('href', '/lan/porto-ca.crt');
    expect(screen.getByTestId('ca-profile')).toHaveAttribute('href', '/lan/porto-ca.mobileconfig');
    expect(screen.getByText(/Android: Impostazioni/)).toBeInTheDocument();
    expect(screen.queryByTestId('ca-fingerprint')).toBeNull();
  });

  it('shows a fingerprint passed by the pairing page, and nothing that is not one', () => {
    expect(fingerprintFromSearch('?fp=ab:cd:ef:01:23:45:67:89')).toBe('AB:CD:EF:01:23:45:67:89');
    expect(fingerprintFromSearch('?fp=<script>')).toBeNull();
    expect(fingerprintFromSearch('')).toBeNull();
  });
});
