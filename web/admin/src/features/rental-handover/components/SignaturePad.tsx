import { EraserIcon } from 'lucide-react';
import { useEffect, useRef, useState, type PointerEvent } from 'react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';

import { canvasPngBase64 } from '../lib/canvas';

/**
 * Signature drawn on screen with pointer events (finger, pen or mouse) on a canvas (m11-plan §7 step 2, D14). The
 * ink takes the foreground colour of the theme (currentColor of the text-foreground class). onChange receives the
 * PNG as base64 after each stroke, or null after "Cancella".
 */
export function SignaturePad({
  onChange,
  label,
}: {
  onChange: (png: string | null) => void;
  label: string;
}) {
  const { t } = useT();
  const ref = useRef<HTMLCanvasElement>(null);
  const drawing = useRef(false);
  const [empty, setEmpty] = useState(true);

  useEffect(() => {
    const c = ref.current;
    if (!c) return;
    const rect = c.getBoundingClientRect();
    c.width = Math.max(1, Math.round(rect.width)) || 600;
    c.height = Math.max(1, Math.round(rect.height)) || 200;
  }, []);

  const ctx = () => {
    const c = ref.current;
    const g = c?.getContext('2d');
    if (!c || !g) return null;
    g.lineWidth = 2.5;
    g.lineCap = 'round';
    g.lineJoin = 'round';
    g.strokeStyle = getComputedStyle(c).color;
    return g;
  };
  const point = (e: PointerEvent<HTMLCanvasElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    const sx = e.currentTarget.width / (r.width || 1);
    const sy = e.currentTarget.height / (r.height || 1);
    return { x: (e.clientX - r.left) * sx, y: (e.clientY - r.top) * sy };
  };
  const down = (e: PointerEvent<HTMLCanvasElement>) => {
    const g = ctx();
    if (!g) return;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    drawing.current = true;
    const p = point(e);
    g.beginPath();
    g.moveTo(p.x, p.y);
    g.lineTo(p.x + 0.1, p.y + 0.1);
    g.stroke();
  };
  const move = (e: PointerEvent<HTMLCanvasElement>) => {
    if (!drawing.current) return;
    const g = ctx();
    if (!g) return;
    const p = point(e);
    g.lineTo(p.x, p.y);
    g.stroke();
  };
  const up = () => {
    if (!drawing.current) return;
    drawing.current = false;
    setEmpty(false);
    if (ref.current) onChange(canvasPngBase64(ref.current));
  };
  const clear = () => {
    const c = ref.current;
    c?.getContext('2d')?.clearRect(0, 0, c.width, c.height);
    setEmpty(true);
    onChange(null);
  };
  return (
    <div className="flex flex-col gap-2">
      <canvas
        ref={ref}
        role="img"
        aria-label={label}
        data-testid="signature-pad"
        className="bg-background text-foreground h-48 w-full touch-none rounded-md border"
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerLeave={up}
      />
      <Button variant="ghost" className="min-h-11 self-end" disabled={empty} onClick={clear}>
        <EraserIcon />
        {t('rentalHandover:sign.clear')}
      </Button>
    </div>
  );
}
