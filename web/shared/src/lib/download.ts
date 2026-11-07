/**
 * Downloads and printing for every page (m6a-plan §4.4): inside the desktop window they go through the bridge
 * (`window.portoDesktop`), elsewhere through an anchor download and `window.print()`.
 */
import { desktopBridge } from '../desktop/bridge';

/** Reads a blob as base64 (no data: prefix), the wire format of the bridge `saveFile`. */
export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error('read failed'));
    reader.onload = () => {
      const result = typeof reader.result === 'string' ? reader.result : '';
      const comma = result.indexOf(',');
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.readAsDataURL(blob);
  });
}

function anchorDownload(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement('a');
    a.href = url;
    a.download = fileName;
    a.rel = 'noopener';
    document.body.appendChild(a);
    a.click();
    a.remove();
  } finally {
    // Revoke after the click has been handled: some browsers read the URL asynchronously.
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
}

/**
 * Saves `blob` as `fileName`: Downloads folder (revealed by the launcher) in the desktop window, a browser
 * download elsewhere. Rejects when the bridge refuses the file (for example over its 50 MiB limit).
 */
export async function saveBlob(blob: Blob, fileName: string): Promise<void> {
  const bridge = desktopBridge();
  if (bridge) {
    await bridge.saveFile(fileName, await blobToBase64(blob));
    return;
  }
  anchorDownload(blob, fileName);
}

/** Saves a text value (JSON, CSV, recovery codes) as a file. */
export function saveText(text: string, fileName: string, type = 'text/plain'): Promise<void> {
  return saveBlob(new Blob([text], { type }), fileName);
}

/** Prints the current page (native print operation in the desktop window). */
export async function printPage(): Promise<void> {
  const bridge = desktopBridge();
  if (bridge) {
    await bridge.print();
    return;
  }
  window.print();
}

/**
 * Shows a generated document (PDF, printable HTML receipt) for printing: a new browser tab outside the desktop
 * window; inside it (the webview opens no tabs) the file is saved to Downloads and revealed, so the system
 * viewer prints it. A blob without a type is treated as a PDF.
 */
export async function openFile(blob: Blob, fileName?: string): Promise<void> {
  const typed = blob.type ? blob : new Blob([blob], { type: 'application/pdf' });
  if (desktopBridge()) {
    const ext = typed.type.startsWith('text/html') ? 'html' : 'pdf';
    await saveBlob(typed, fileName ?? `porto-document.${ext}`);
    return;
  }
  const url = URL.createObjectURL(typed);
  window.open(url, '_blank', 'noopener');
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

/** True for the links the app may open outside itself (the bridge refuses everything else too). */
export function isExternalUrl(url: string): boolean {
  try {
    const { protocol } = new URL(url);
    return protocol === 'https:' || protocol === 'mailto:';
  } catch {
    return false;
  }
}

/** Opens an external `https:`/`mailto:` link: the default browser from the desktop window, a new tab elsewhere. */
export async function openExternal(url: string): Promise<void> {
  if (!isExternalUrl(url)) throw new Error('only https: and mailto: links open outside the app');
  const bridge = desktopBridge();
  if (bridge) {
    await bridge.openExternal(url);
    return;
  }
  window.open(url, '_blank', 'noopener,noreferrer');
}
