import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { TestProviders } from '../testing';
import {
  CameraScanner,
  FRAME_INTERVAL_MS,
  createWedgeDetector,
  isTextTarget,
  parseTransitQr,
  useKeyboardWedge,
} from './index';

const jsqrMock = vi.hoisted(() => vi.fn());
vi.mock('jsqr', () => ({ default: jsqrMock }));

const ID = '0190a000-0000-7000-8000-00000000f001';

describe('parseTransitQr', () => {
  it.each([
    [`PT1:${ID}`, ID],
    [`  PT1:${ID.toUpperCase()}\n`, ID],
  ])('accepts %j', (text, id) => {
    expect(parseTransitQr(text)).toEqual({ contractId: id });
  });
  it.each([
    '',
    'PT1:',
    `PT2:${ID}`,
    `pt1:${ID}`,
    `PT1:${ID}x`,
    `https://x/PT1:${ID}`,
    'PT1:not-a-uuid',
  ])('refuses %j', (text) => {
    expect(parseTransitQr(text)).toBeNull();
  });
});

describe('keyboard wedge detector', () => {
  const feed = (
    d: ReturnType<typeof createWedgeDetector>,
    text: string,
    start: number,
    gap: number,
  ) => {
    let at = start;
    for (const ch of text) {
      expect(d.key(ch, at)).toBeNull();
      at += gap;
    }
    return d.key('Enter', at);
  };

  it('reports a fast burst of at least 8 characters ending with Enter', () => {
    const d = createWedgeDetector();
    expect(feed(d, `PT1:${ID}`, 1000, 5)).toBe(`PT1:${ID}`);
    // Exactly 8 characters at the 50 ms limit.
    expect(feed(d, 'ABCDEFGH', 5000, 50)).toBe('ABCDEFGH');
  });

  it('ignores slow typing, short bursts and a late Enter', () => {
    const d = createWedgeDetector();
    expect(feed(d, 'ABCDEFGHIJ', 0, 51)).toBeNull();
    expect(feed(d, 'ABCDEFG', 10_000, 5)).toBeNull();
    for (let i = 0; i < 10; i++) d.key('X', 20_000 + i * 5);
    expect(d.key('Enter', 20_000 + 9 * 5 + 200)).toBeNull();
  });

  it('restarts the burst after a pause and keeps it across modifier keys', () => {
    const d = createWedgeDetector();
    d.key('Z', 0);
    d.key('Z', 10);
    let at = 500;
    for (const ch of 'PT1:ABCD') {
      d.key('Shift', at);
      d.key(ch, at + 2);
      at += 10;
    }
    expect(d.key('Enter', at)).toBe('PT1:ABCD');
  });

  it('recognises text targets', () => {
    const input = document.createElement('input');
    const checkbox = document.createElement('input');
    checkbox.type = 'checkbox';
    expect(isTextTarget(input)).toBe(true);
    expect(isTextTarget(document.createElement('textarea'))).toBe(true);
    expect(isTextTarget(checkbox)).toBe(false);
    expect(isTextTarget(document.createElement('button'))).toBe(false);
    expect(isTextTarget(null)).toBe(false);
  });
});

function Wedge({ onScan }: { onScan: (t: string) => void }) {
  useKeyboardWedge(onScan);
  return <input aria-label="campo" />;
}

describe('useKeyboardWedge', () => {
  it('scans from the page and ignores keys typed in a text field', () => {
    const onScan = vi.fn();
    render(<Wedge onScan={onScan} />);
    const type = (target: HTMLElement, text: string) => {
      let at = 100;
      for (const ch of text) {
        fireEvent.keyDown(target, { key: ch, timeStamp: at });
        at += 5;
      }
      const enter = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
      Object.defineProperty(enter, 'timeStamp', { value: at });
      target.dispatchEvent(enter);
      return enter;
    };
    const enter = type(document.body, `PT1:${ID}`);
    expect(onScan).toHaveBeenCalledWith(`PT1:${ID}`);
    expect(enter.defaultPrevented).toBe(true);
    onScan.mockClear();
    type(screen.getByLabelText('campo'), `PT1:${ID}`);
    expect(onScan).not.toHaveBeenCalled();
  });
});

// ---- camera ----

type FakeTrack = {
  stop: ReturnType<typeof vi.fn>;
  getCapabilities: () => object;
  applyConstraints: ReturnType<typeof vi.fn>;
};

function fakeCamera(torch = false) {
  const track: FakeTrack = {
    stop: vi.fn(),
    getCapabilities: () => (torch ? { torch: true } : {}),
    applyConstraints: vi.fn(async () => {}),
  };
  const stream = { getTracks: () => [track], getVideoTracks: () => [track] };
  const getUserMedia = vi.fn(async () => stream);
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } });
  return { track, getUserMedia };
}

function readyVideo() {
  Object.defineProperty(HTMLMediaElement.prototype, 'readyState', {
    configurable: true,
    get: () => 4,
  });
  Object.defineProperty(HTMLVideoElement.prototype, 'videoWidth', {
    configurable: true,
    get: () => 64,
  });
  Object.defineProperty(HTMLVideoElement.prototype, 'videoHeight', {
    configurable: true,
    get: () => 48,
  });
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
}

const renderScanner = (props: Parameters<typeof CameraScanner>[0]) =>
  render(
    <TestProviders>
      <CameraScanner {...props} />
    </TestProviders>,
  );

describe('CameraScanner', () => {
  beforeEach(() => {
    readyVideo();
    jsqrMock.mockReset();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    delete (globalThis as { BarcodeDetector?: unknown }).BarcodeDetector;
  });

  it('decodes frames with the BarcodeDetector, reports a text once, stops the camera on unmount', async () => {
    const { track, getUserMedia } = fakeCamera(true);
    const detect = vi.fn(async () => [{ rawValue: `PT1:${ID}` }]);
    (globalThis as { BarcodeDetector?: unknown }).BarcodeDetector = class {
      constructor(opts: { formats: string[] }) {
        expect(opts.formats).toEqual(['qr_code']);
      }
      detect = detect;
    };
    const onScan = vi.fn();
    const { unmount } = renderScanner({ onScan });
    await waitFor(() => expect(onScan).toHaveBeenCalledWith(`PT1:${ID}`));
    expect(getUserMedia).toHaveBeenCalledWith(
      expect.objectContaining({ video: { facingMode: { ideal: 'environment' } } }),
    );
    // Further frames with the same text are not reported again right away.
    await waitFor(() => expect(detect.mock.calls.length).toBeGreaterThan(1), {
      timeout: FRAME_INTERVAL_MS * 5,
    });
    expect(onScan).toHaveBeenCalledTimes(1);
    expect(jsqrMock).not.toHaveBeenCalled();
    // Torch toggle when supported.
    const torch = await screen.findByRole('button', { name: 'Torcia' });
    fireEvent.click(torch);
    await waitFor(() =>
      expect(track.applyConstraints).toHaveBeenCalledWith({ advanced: [{ torch: true }] }),
    );
    unmount();
    expect(track.stop).toHaveBeenCalled();
  });

  it('falls back to jsQR on canvas frames without a BarcodeDetector', async () => {
    fakeCamera();
    const ctx = {
      drawImage: vi.fn(),
      getImageData: vi.fn((_x: number, _y: number, w: number, h: number) => ({
        data: new Uint8ClampedArray(w * h * 4),
        width: w,
        height: h,
      })),
    };
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(
      ctx as unknown as CanvasRenderingContext2D,
    );
    jsqrMock.mockReturnValueOnce(null).mockReturnValue({ data: `PT1:${ID}` });
    const onScan = vi.fn();
    renderScanner({ onScan });
    await waitFor(() => expect(onScan).toHaveBeenCalledWith(`PT1:${ID}`), { timeout: 2000 });
    expect(jsqrMock).toHaveBeenCalledWith(expect.any(Uint8ClampedArray), 64, 48, expect.anything());
    expect(screen.queryByRole('button', { name: 'Torcia' })).not.toBeInTheDocument();
  });

  it('does not decode while paused', async () => {
    fakeCamera();
    const detect = vi.fn(async () => [{ rawValue: `PT1:${ID}` }]);
    (globalThis as { BarcodeDetector?: unknown }).BarcodeDetector = class {
      detect = detect;
    };
    const onScan = vi.fn();
    renderScanner({ onScan, paused: true });
    await act(async () => {
      await new Promise((r) => setTimeout(r, FRAME_INTERVAL_MS * 3));
    });
    expect(detect).not.toHaveBeenCalled();
    expect(onScan).not.toHaveBeenCalled();
  });

  it('reports a denied camera and keeps the photo fallback', async () => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        getUserMedia: vi.fn(async () => {
          throw Object.assign(new Error('no'), { name: 'NotAllowedError' });
        }),
      },
    });
    const onError = vi.fn();
    renderScanner({ onScan: vi.fn(), onError });
    await waitFor(() => expect(onError).toHaveBeenCalledWith('denied'));
    expect(screen.getByTestId('camera-scanner-failure')).toHaveTextContent(
      'Accesso alla fotocamera negato',
    );
    expect(screen.getByText('Scegli una foto del QR')).toBeInTheDocument();
  });

  it('reports a missing camera API as unavailable', async () => {
    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: undefined });
    const onError = vi.fn();
    renderScanner({ onScan: vi.fn(), onError });
    await waitFor(() => expect(onError).toHaveBeenCalledWith('unavailable'));
  });

  it('decodes a chosen photo', async () => {
    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: undefined });
    const detect = vi.fn(async () => [{ rawValue: `PT1:${ID}` }]);
    (globalThis as { BarcodeDetector?: unknown }).BarcodeDetector = class {
      detect = detect;
    };
    (globalThis as { createImageBitmap?: unknown }).createImageBitmap = vi.fn(async () => ({
      width: 10,
      height: 10,
    }));
    const onScan = vi.fn();
    renderScanner({ onScan });
    fireEvent.change(screen.getByTestId('camera-scanner-file'), {
      target: { files: [new File(['x'], 'qr.png', { type: 'image/png' })] },
    });
    await waitFor(() => expect(onScan).toHaveBeenCalledWith(`PT1:${ID}`));
  });

  it('says so when the photo has no QR', async () => {
    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: undefined });
    (globalThis as { BarcodeDetector?: unknown }).BarcodeDetector = class {
      detect = vi.fn(async () => []);
    };
    (globalThis as { createImageBitmap?: unknown }).createImageBitmap = vi.fn(async () => ({
      width: 10,
      height: 10,
    }));
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
      drawImage: vi.fn(),
      getImageData: () => ({ data: new Uint8ClampedArray(400) }),
    } as unknown as CanvasRenderingContext2D);
    jsqrMock.mockReturnValue(null);
    renderScanner({ onScan: vi.fn() });
    fireEvent.change(screen.getByTestId('camera-scanner-file'), {
      target: { files: [new File(['x'], 'qr.png', { type: 'image/png' })] },
    });
    expect(await screen.findByRole('alert')).toHaveTextContent('Nessun QR trovato nella foto');
  });
});
