// Browser file helpers of the M4 pages: save a CSV/PDF export, open a PDF in a new tab.
/** Saves a blob as a file through a temporary object URL. */
export function saveBlob(filename: string, blob: Blob): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** Opens a PDF blob in a new tab (print from the browser). */
export function openBlob(blob: Blob): void {
  const url = URL.createObjectURL(blob.type ? blob : new Blob([blob], { type: 'application/pdf' }));
  window.open(url, '_blank', 'noopener');
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}
