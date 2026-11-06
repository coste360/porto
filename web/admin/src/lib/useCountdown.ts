import { useEffect, useState } from 'react';

/** Whole seconds left until `expiresAt` (RFC 3339), ticking every second; 0 when past. */
export function useCountdown(expiresAt: string | null): number {
  const target = expiresAt ? Date.parse(expiresAt) : NaN;
  const compute = () =>
    Number.isNaN(target) ? 0 : Math.max(0, Math.ceil((target - Date.now()) / 1000));
  const [left, setLeft] = useState(compute);
  useEffect(() => {
    if (Number.isNaN(target)) return undefined;
    const tick = () => setLeft(Math.max(0, Math.ceil((target - Date.now()) / 1000)));
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, [target]);
  return Number.isNaN(target) ? 0 : left;
}
