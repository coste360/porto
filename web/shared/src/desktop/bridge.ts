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
