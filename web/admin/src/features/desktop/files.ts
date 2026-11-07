// Fire-and-forget wrappers of the shared download helpers for the admin call sites (m6a-plan §4.4): every
// download and printable document goes through `@porto/shared/lib/download`, which uses the desktop bridge when
// present. A refused save (desktop window, file over the bridge limit) becomes a toast instead of an unhandled
// rejection.
import { getI18n } from 'react-i18next';
import { toast } from 'sonner';

import { openFile, saveBlob } from '@porto/shared/lib/download';

function failed(): void {
  toast.error(getI18n()?.t('desktop:download.failed') ?? 'desktop:download.failed');
}

/** Saves `blob` as `fileName` (Downloads folder in the desktop window, browser download elsewhere). */
export function downloadFile(fileName: string, blob: Blob): void {
  saveBlob(blob, fileName).catch(failed);
}

/** Opens a generated PDF or printable HTML (new tab, or saved and revealed in the desktop window). */
export function showFile(blob: Blob, fileName?: string): void {
  openFile(blob, fileName).catch(failed);
}
