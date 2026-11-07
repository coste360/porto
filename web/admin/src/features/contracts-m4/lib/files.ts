// Browser file helpers of the M4 pages: save a CSV/PDF export, open a PDF in a new tab. Both go through the
// shared download helpers, so they work in the desktop window too (m6a-plan §4.4).
import { downloadFile, showFile } from '../../desktop/files';

/** Saves a blob as a file. */
export function saveBlob(filename: string, blob: Blob): void {
  downloadFile(filename, blob);
}

/** Opens a PDF blob for printing (new tab in the browser; saved and revealed in the desktop window). */
export function openBlob(blob: Blob): void {
  showFile(blob);
}
