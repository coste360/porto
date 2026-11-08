import { createRoute } from '@tanstack/react-router';

import type { PlainRoute, RootRoute } from '../../router';
import { registerDockSlots } from '../phone/slots';
import { BridgeReceiptButton } from './BridgeReceiptButton';
import { DesktopSettingsPage } from './DesktopSettingsPage';
import { FirstRunPage } from './FirstRunPage';
import { KioskExitPage } from './KioskExitPage';
import { MoveOnlinePage } from './MoveOnlinePage';
import { RemotePairPage } from './RemotePairPage';

/**
 * Desktop edition routes (m6a-plan §7): the first run is outside the auth guard (child of the root route); the
 * settings and the move-online guide are inside it (children of the plain layout, not marina-scoped).
 */
export function desktopRoutes(root: RootRoute, plain: PlainRoute) {
  // M6b: "Stampa scontrino" on the local receipt printer next to each payment of the dock Cassa tab (PHONE's
  // slot; the button renders nothing outside the app window).
  registerDockSlots({
    receiptAction: (paymentId) => <BridgeReceiptButton paymentId={paymentId} size="sm" />,
  });
  const firstRun = createRoute({
    getParentRoute: () => root,
    path: 'desktop/first-run',
    component: FirstRunPage,
  });
  const settings = createRoute({
    getParentRoute: () => plain,
    path: 'desktop',
    component: DesktopSettingsPage,
  });
  const online = createRoute({
    getParentRoute: () => plain,
    path: 'desktop/online',
    component: MoveOnlinePage,
  });
  // M6b: pairing to an online instance (first-run branch) and the kiosk exit, outside the auth guard (the remote
  // mode has no local session; the pages call the loopback API with the first-run token or the app PIN).
  const remote = createRoute({
    getParentRoute: () => root,
    path: 'desktop/remote',
    component: RemotePairPage,
  });
  const kioskExit = createRoute({
    getParentRoute: () => root,
    path: 'desktop/kiosk-exit',
    component: KioskExitPage,
  });
  return {
    publicRoutes: [firstRun, remote, kioskExit] as const,
    plainRoutes: [settings, online] as const,
  };
}
