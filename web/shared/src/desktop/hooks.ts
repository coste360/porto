// Hooks of the M6b desktop surface (m6b-plan §7, §8): the app-window status in both modes, the peripheral
// settings, kiosk, remote pairing and unpairing, and the bridge to the local peripherals. Like the M6a desktop
// hooks they live under ['desktop'] / ['bridge'], are not marina-scoped and never cached offline. The app PIN of the
// remote mode travels only in the Porto-App-Pin header of the call that needs it; nothing here stores it.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { unwrap } from '../api/client';
import { useApi } from '../api/context';
import { useInstanceMeta } from '../api/hooks/desktop';
import { ProblemError } from '../api/problem';
import { queryKeys } from '../api/queryKeys';
import type { components } from '../api/schema';
import { createBridgeClient, isDesktopWindow, type BridgeStatus } from './bridge';

type S = components['schemas'];

const D = '/api/desktop/v1' as const;

export type PeripheralSettings = S['PeripheralSettings'];
export type DesktopRemotePairRequest = S['DesktopRemotePairRequest'];
export type RemoteStatus = S['RemoteStatus'];
export type DesktopKioskExit = S['DesktopKioskExit'];

/** Header of the app PIN (remote mode, security scheme appPin). */
export const APP_PIN_HEADER = 'Porto-App-Pin';

const pinHeaders = (pin?: string) => (pin ? { [APP_PIN_HEADER]: pin } : undefined);

/** True when the page is served from the loopback origin (the app window, or its proxy in remote mode). */
export function isLoopbackOrigin(): boolean {
  if (typeof window === 'undefined') return false;
  const h = window.location.hostname;
  return h === '127.0.0.1' || h === 'localhost' || h === '[::1]';
}

/**
 * Status of the app window in both modes. In local mode the instance is the desktop edition; in remote mode the
 * page talks to an online instance (edition "server") through the app's proxy on the loopback origin, which
 * answers /api/desktop/v1 itself. Elsewhere the call is a 404 and the status is null.
 */
export function useAppWindowStatus() {
  const api = useApi();
  const meta = useInstanceMeta();
  const candidate = meta.data?.edition === 'desktop' || isDesktopWindow() || isLoopbackOrigin();
  const query = useQuery({
    queryKey: queryKeys.desktop.status,
    enabled: candidate,
    retry: false,
    staleTime: 60_000,
    queryFn: async () => {
      try {
        return await unwrap(api.GET(`${D}/status`));
      } catch (error) {
        if (error instanceof ProblemError && error.status === 404) return null;
        throw error;
      }
    },
  });
  const data = query.data ?? null;
  return {
    ...query,
    data,
    /** True in the app window (local or remote mode). */
    loopback: candidate && !!data,
    /** True in the app window in remote mode. */
    remote: data?.mode === 'remote',
    resolving: meta.isPending || (candidate && query.isPending),
  };
}

/** Peripheral settings (local mode: owner; remote mode: any user of the window). */
export function useDesktopPeripherals(enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.desktop.peripherals,
    enabled,
    queryFn: () => unwrap(api.GET(`${D}/peripherals`)),
  });
}

export function useUpdateDesktopPeripherals() {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { settings: PeripheralSettings; pin?: string }) =>
      unwrap(api.PUT(`${D}/peripherals`, { body: v.settings, headers: pinHeaders(v.pin) })),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.desktop.peripherals, data);
      void qc.invalidateQueries({ queryKey: queryKeys.bridge.all });
    },
  });
}

/** Kiosk at the next launch (local mode: owner; remote mode: app PIN). */
export function useSetDesktopKiosk() {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { enabled: boolean; pin?: string }) =>
      unwrap(api.PUT(`${D}/kiosk`, { body: { enabled: v.enabled }, headers: pinHeaders(v.pin) })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.desktop.all }),
  });
}

/** Leaves kiosk mode until the next launch (local mode: owner credentials; remote mode: app PIN). */
export function useExitDesktopKiosk() {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { pin?: string; credentials?: DesktopKioskExit }) =>
      unwrap(
        api.POST(`${D}/kiosk/exit`, {
          body: v.credentials ?? {},
          headers: pinHeaders(v.pin),
        }),
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.desktop.status }),
  });
}

/** First run: pairs the app with an online instance; the app then restarts in remote mode (202). */
export function usePairDesktopRemote() {
  const api = useApi();
  return useMutation({
    mutationFn: (body: DesktopRemotePairRequest) => unwrap(api.POST(`${D}/remote/pair`, { body })),
  });
}

/** Remote mode: unpairs the app (app PIN); the app restarts on the first run (202). */
export function useUnpairDesktopRemote() {
  const api = useApi();
  return useMutation({
    mutationFn: (pin: string) => unwrap(api.DELETE(`${D}/remote`, { headers: pinHeaders(pin) })),
  });
}

/**
 * Status of the local peripherals; enabled only in the app window (the bridge answers there only). Polled every
 * 30 s so a printer switched on later shows up.
 */
export function useBridgeStatus(enabled: boolean) {
  const api = useApi();
  return useQuery<BridgeStatus>({
    queryKey: queryKeys.bridge.status,
    enabled,
    retry: false,
    staleTime: 15_000,
    refetchInterval: enabled ? 30_000 : false,
    queryFn: () => createBridgeClient(api).status(),
  });
}

/** True when the receipt printer of the app window is ready (hides "Stampa scontrino" elsewhere). */
export function useReceiptPrinterReady(): boolean {
  const win = useAppWindowStatus();
  const bridge = useBridgeStatus(win.loopback);
  return win.loopback && bridge.data?.receipt.ready === true;
}

export function usePrintBridgeReceipt() {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { paymentId: string; width?: 80 | 58 }) =>
      createBridgeClient(api).printReceipt(v.paymentId, v.width),
  });
}

/** Test page of a printer or the status of the local fiscal device (local mode: owner; remote mode: PIN). */
export function useTestBridgePeripheral() {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { device: 'receipt' | 'label' | 'fiscal'; pin?: string }) =>
      unwrap(
        api.POST('/api/bridge/v1/test', { body: { device: v.device }, headers: pinHeaders(v.pin) }),
      ),
  });
}
