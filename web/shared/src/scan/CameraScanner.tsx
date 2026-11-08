// Camera QR scanner (m6b-plan §6.4): rear camera in a <video>, frames decoded at most 10 times a second with the
// BarcodeDetector or jsQR, torch toggle when the track supports it, and a "choose a photo" fallback (also used
// when the camera cannot be opened, and by the end-to-end tests). Tracks are stopped on unmount.
import { FlashlightIcon, ImageIcon } from 'lucide-react';
import { useEffect, useId, useRef, useState, type ReactElement } from 'react';

import { useT } from '../i18n/useT';
import { Button } from '@porto/shared/ui/button';

import {
  decodeImageFile,
  decodePixels,
  qrBarcodeDetector,
  type BarcodeDetectorLike,
} from './decode';

export type CameraScannerProps = {
  /** Called with the decoded text of each QR (the caller filters with parseTransitQr). */
  onScan: (text: string) => void;
  /** Called when the camera cannot be used (permission denied, no device, insecure context). */
  onError?: (reason: 'denied' | 'unavailable' | 'insecure') => void;
  /** Pauses decoding without releasing the camera. */
  paused?: boolean;
};

/** Minimum time between frames (10 fps at most). */
export const FRAME_INTERVAL_MS = 100;
/** The same text is reported again only after this pause (a QR stays in front of the camera for a while). */
const REPEAT_MS = 2000;

type TorchCapabilities = MediaTrackCapabilities & { torch?: boolean };

function cameraFailure(err: unknown): 'denied' | 'unavailable' {
  const name = (err as { name?: string } | null)?.name;
  return name === 'NotAllowedError' || name === 'SecurityError' ? 'denied' : 'unavailable';
}

export function CameraScanner({
  onScan,
  onError,
  paused = false,
}: CameraScannerProps): ReactElement {
  const { t } = useT();
  const fileId = useId();
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const trackRef = useRef<MediaStreamTrack | null>(null);
  const pausedRef = useRef(paused);
  const onScanRef = useRef(onScan);
  const onErrorRef = useRef(onError);
  useEffect(() => {
    pausedRef.current = paused;
    onScanRef.current = onScan;
    onErrorRef.current = onError;
  });
  const lastRef = useRef<{ text: string; at: number } | null>(null);
  const [failure, setFailure] = useState<'denied' | 'unavailable' | 'insecure' | null>(null);
  const [torchSupported, setTorchSupported] = useState(false);
  const [torchOn, setTorchOn] = useState(false);
  const [fileInvalid, setFileInvalid] = useState(false);

  const emit = (text: string) => {
    const now = Date.now();
    const last = lastRef.current;
    if (last && last.text === text && now - last.at < REPEAT_MS) return;
    lastRef.current = { text, at: now };
    onScanRef.current(text);
  };

  useEffect(() => {
    let cancelled = false;
    let stream: MediaStream | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const fail = (reason: 'denied' | 'unavailable' | 'insecure') => {
      if (cancelled) return;
      setFailure(reason);
      onErrorRef.current?.(reason);
    };
    if (typeof window !== 'undefined' && window.isSecureContext === false) {
      fail('insecure');
      return;
    }
    const media = typeof navigator !== 'undefined' ? navigator.mediaDevices : undefined;
    if (!media?.getUserMedia) {
      fail('unavailable');
      return;
    }
    const detector: BarcodeDetectorLike | null = qrBarcodeDetector();
    const canvas = document.createElement('canvas');
    const tick = async () => {
      const video = videoRef.current;
      if (cancelled) return;
      if (video && !pausedRef.current && video.readyState >= 2 && video.videoWidth > 0) {
        try {
          let text: string | null = null;
          if (detector) {
            const found = await detector.detect(video);
            text = found[0]?.rawValue ?? null;
          } else {
            const w = video.videoWidth;
            const h = video.videoHeight;
            canvas.width = w;
            canvas.height = h;
            const ctx = canvas.getContext('2d', { willReadFrequently: true });
            if (ctx) {
              ctx.drawImage(video, 0, 0, w, h);
              text = decodePixels(ctx.getImageData(0, 0, w, h).data, w, h);
            }
          }
          if (text && !cancelled) emit(text);
        } catch {
          // A frame that cannot be decoded is skipped.
        }
      }
      if (!cancelled) timer = setTimeout(() => void tick(), FRAME_INTERVAL_MS);
    };
    media
      .getUserMedia({ video: { facingMode: { ideal: 'environment' } }, audio: false })
      .then(async (s) => {
        if (cancelled) {
          s.getTracks().forEach((tr) => tr.stop());
          return;
        }
        stream = s;
        const track = s.getVideoTracks()[0] ?? null;
        trackRef.current = track;
        const caps = track?.getCapabilities?.() as TorchCapabilities | undefined;
        setTorchSupported(caps?.torch === true);
        const video = videoRef.current;
        if (video) {
          video.srcObject = s;
          try {
            await video.play();
          } catch {
            // Autoplay refused: frames still arrive once the user taps the video.
          }
        }
        void tick();
      })
      .catch((err: unknown) => fail(cameraFailure(err)));
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      stream?.getTracks().forEach((tr) => tr.stop());
      trackRef.current = null;
    };
  }, []);

  const toggleTorch = async () => {
    const track = trackRef.current;
    if (!track) return;
    const next = !torchOn;
    try {
      await track.applyConstraints({ advanced: [{ torch: next } as MediaTrackConstraintSet] });
      setTorchOn(next);
    } catch {
      setTorchSupported(false);
    }
  };

  return (
    <div className="flex flex-col gap-3" data-testid="camera-scanner">
      {failure === null ? (
        <div className="bg-muted relative aspect-square w-full overflow-hidden rounded-md border">
          <video
            ref={videoRef}
            className="size-full object-cover"
            muted
            playsInline
            aria-label={t('phone:scan.start')}
          />
        </div>
      ) : (
        <p
          className="text-muted-foreground text-sm"
          role="status"
          data-testid="camera-scanner-failure"
        >
          {failure === 'denied' ? t('phone:scan.cameraDenied') : t('phone:scan.cameraUnavailable')}
        </p>
      )}
      <p className="text-muted-foreground text-sm">{t('phone:scan.start')}</p>
      <div className="flex flex-wrap gap-2">
        {torchSupported && failure === null ? (
          <Button
            type="button"
            variant={torchOn ? 'default' : 'outline'}
            className="min-h-11"
            aria-pressed={torchOn}
            onClick={() => void toggleTorch()}
          >
            <FlashlightIcon />
            {t('phone:scan.torch')}
          </Button>
        ) : null}
        <Button type="button" variant="outline" className="min-h-11" asChild>
          <label htmlFor={fileId}>
            <ImageIcon />
            {t('phone:scan.chooseImage')}
          </label>
        </Button>
        <input
          id={fileId}
          type="file"
          accept="image/*"
          className="sr-only"
          data-testid="camera-scanner-file"
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = '';
            if (!file) return;
            setFileInvalid(false);
            void decodeImageFile(file)
              .then((text) => {
                if (text) {
                  lastRef.current = null;
                  emit(text);
                } else setFileInvalid(true);
              })
              .catch(() => setFileInvalid(true));
          }}
        />
      </div>
      {fileInvalid ? (
        <p className="text-destructive text-sm" role="alert">
          {t('phone:scan.noQrInImage')}
        </p>
      ) : null}
    </div>
  );
}
