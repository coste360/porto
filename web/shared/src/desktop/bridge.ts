import { unwrap, type ApiClient } from '../api/client';
import type { components } from '../api/schema';

/**
 * JS bridge of the desktop window (m6a-plan §2.13). `porto-desktop` built with the webview injects
 * `window.portoDesktop` before the page loads; in the browser and on local-network clients it is absent and the
 * web keeps its native behaviour (anchor downloads, `window.print()`).
 *
 * The binding names are part of the M6a contract (implemented by the launcher, used through this module only).
 */
export type PortoDesktopBridge = {
  /** Version of the running launcher. */
  readonly version: string;
  /**
   * Writes a file into the user's Downloads folder with a unique, sanitised name (at most 50 MiB) and reveals it.
   * `base64` is the file content. Resolves with the saved path.
   */
  saveFile(name: string, base64: string): Promise<string>;
  /** Opens an `https:` or `mailto:` URL in the default browser; anything else is refused. */
  openExternal(url: string): Promise<void>;
  /** Runs the native print operation of the window. */
  print(): Promise<void>;
};

declare global {
  interface Window {
    portoDesktop?: PortoDesktopBridge;
  }
}

/** The bridge when the page runs inside the desktop window, else undefined. */
export function desktopBridge(): PortoDesktopBridge | undefined {
  if (typeof window === 'undefined') return undefined;
  return window.portoDesktop;
}

/** True inside the desktop app window (not in a browser, not on a local-network client). */
export function isDesktopWindow(): boolean {
  return desktopBridge() !== undefined;
}

// ---- M6b: HTTP bridge to the local peripherals (/api/bridge/v1, m6b-plan §8.3) ----
// Loopback only: in the app window (local mode: in-process server; remote mode: the app's proxy). Elsewhere every
// call is 404, so callers check `status()` first and show "Stampa scontrino" only when the receipt printer is ready.

export type BridgeStatus = components['schemas']['BridgeStatus'];
export type PeripheralStatus = components['schemas']['PeripheralStatus'];

export type BridgeClient = {
  /** Status of the receipt and label printers, the QR reader and the local fiscal device. */
  status(): Promise<BridgeStatus>;
  /** Prints the courtesy receipt of a payment on the local receipt printer. */
  printReceipt(paymentId: string, width?: 80 | 58): Promise<void>;
  /** Waits up to `timeoutSeconds` (1..25) for a scan of a non-keyboard reader; null when none arrived. */
  nextScan(timeoutSeconds?: number): Promise<string | null>;
};

/** The bridge client over the panel's API client. */
export function createBridgeClient(api: ApiClient): BridgeClient {
  return {
    status: () => unwrap(api.GET('/api/bridge/v1/status')),
    printReceipt: async (paymentId, width) => {
      await unwrap(api.POST('/api/bridge/v1/receipts', { body: { payment_id: paymentId, width } }));
    },
    nextScan: async (timeoutSeconds = 25) => {
      const res = await api.GET('/api/bridge/v1/scanner/next', {
        params: { query: { timeout: timeoutSeconds } },
      });
      if (res.response.status === 204) return null;
      const data = await unwrap(Promise.resolve(res));
      return data?.text ?? null;
    },
  };
}
