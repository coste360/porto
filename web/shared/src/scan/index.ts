// Transit QR scanning (m6b-plan §6.4): the payload parser, the camera scanner (BarcodeDetector, jsQR fallback,
// photo fallback) and keyboard-wedge USB readers.

/** Prefix of the transit QR payload: PT1:<contract_id> (opaque, no personal data, no URL; D10). */
export const TRANSIT_QR_PREFIX = 'PT1:';

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** The contract id of a transit QR payload, or null when the text is not one. */
export function parseTransitQr(text: string): { contractId: string } | null {
  const t = text.trim();
  if (!t.startsWith(TRANSIT_QR_PREFIX)) return null;
  const id = t.slice(TRANSIT_QR_PREFIX.length);
  return UUID.test(id) ? { contractId: id.toLowerCase() } : null;
}

export { CameraScanner, FRAME_INTERVAL_MS, type CameraScannerProps } from './CameraScanner';
export {
  useKeyboardWedge,
  createWedgeDetector,
  isTextTarget,
  WEDGE_MAX_GAP_MS,
  WEDGE_MIN_CHARS,
} from './wedge';
export {
  decodeImageFile,
  decodePixels,
  qrBarcodeDetector,
  type BarcodeDetectorLike,
} from './decode';
