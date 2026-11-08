// Presence photos queued from the phone (m6b-plan §4.2): re-encoded as JPEG with the longest side at most
// 2048 px before they go into the queue, so a phone camera picture fits the 8 MiB server limit and the device
// storage. When the browser cannot decode or encode the image, the original file is kept (the server checks it).
export const PHOTO_MAX_SIDE = 2048;
export const PHOTO_MAX_BYTES = 8 * 1024 * 1024;
const QUALITY = 0.85;

/** Size of the re-encoded image: the longest side capped at max, the aspect ratio kept. */
export function scaledSize(w: number, h: number, max = PHOTO_MAX_SIDE): { w: number; h: number } {
  const scale = Math.min(1, max / Math.max(w, h, 1));
  return { w: Math.max(1, Math.round(w * scale)), h: Math.max(1, Math.round(h * scale)) };
}

export async function reencodePhoto(file: Blob): Promise<Blob> {
  if (typeof createImageBitmap !== 'function') return file;
  try {
    const bitmap = await createImageBitmap(file);
    const { w, h } = scaledSize(bitmap.width, bitmap.height);
    const canvas = document.createElement('canvas');
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext('2d');
    if (!ctx) return file;
    ctx.drawImage(bitmap, 0, 0, w, h);
    bitmap.close?.();
    const out = await new Promise<Blob | null>((resolve) =>
      canvas.toBlob(resolve, 'image/jpeg', QUALITY),
    );
    return out && out.size <= PHOTO_MAX_BYTES ? out : file;
  } catch {
    return file;
  }
}
