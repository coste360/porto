// QR decoding of camera frames and photos (m6b-plan §6.4): the platform BarcodeDetector when present, else jsQR
// on the pixels. Nothing leaves the device.
import jsQR from 'jsqr';

/** Minimal shape of the Shape Detection API (not in lib.dom yet). */
export type BarcodeDetectorLike = {
  detect(source: CanvasImageSource | ImageBitmap | ImageData): Promise<{ rawValue: string }[]>;
};
type BarcodeDetectorCtor = new (opts?: { formats?: string[] }) => BarcodeDetectorLike;

/** A QR BarcodeDetector, or null when the browser has none (then jsQR is used). */
export function qrBarcodeDetector(): BarcodeDetectorLike | null {
  const Ctor = (globalThis as { BarcodeDetector?: BarcodeDetectorCtor }).BarcodeDetector;
  if (!Ctor) return null;
  try {
    return new Ctor({ formats: ['qr_code'] });
  } catch {
    return null;
  }
}

/** Decodes a QR in raw RGBA pixels with jsQR (null when none). */
export function decodePixels(
  data: Uint8ClampedArray,
  width: number,
  height: number,
): string | null {
  const code = jsQR(data, width, height, { inversionAttempts: 'attemptBoth' });
  return code && code.data ? code.data : null;
}

/** Longest side of the canvas a photo is scaled to before jsQR (enough for a QR, fast on phones). */
const MAX_SIDE = 1024;

async function loadImage(
  file: Blob,
): Promise<CanvasImageSource & { width: number; height: number }> {
  if (typeof createImageBitmap === 'function') return createImageBitmap(file);
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    img.src = url;
    await img.decode();
    return img;
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** Decodes the QR of a photo (the "Scegli una foto del QR" fallback); null when none is found. */
export async function decodeImageFile(
  file: Blob,
  detector: BarcodeDetectorLike | null = qrBarcodeDetector(),
): Promise<string | null> {
  const img = await loadImage(file);
  if (detector) {
    const found = await detector.detect(img as CanvasImageSource);
    if (found[0]?.rawValue) return found[0].rawValue;
  }
  const scale = Math.min(1, MAX_SIDE / Math.max(img.width, img.height));
  const w = Math.max(1, Math.round(img.width * scale));
  const h = Math.max(1, Math.round(img.height * scale));
  const canvas = document.createElement('canvas');
  canvas.width = w;
  canvas.height = h;
  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  if (!ctx) return null;
  ctx.drawImage(img, 0, 0, w, h);
  const pixels = ctx.getImageData(0, 0, w, h);
  return decodePixels(pixels.data, w, h);
}
