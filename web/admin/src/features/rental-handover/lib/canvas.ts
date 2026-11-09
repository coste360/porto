/** Base64 (no data: prefix) of the PNG of a canvas (the drawn signature sent to signRentalContract). */
export function canvasPngBase64(canvas: HTMLCanvasElement): string {
  const url = canvas.toDataURL('image/png');
  const i = url.indexOf(',');
  return i >= 0 ? url.slice(i + 1) : '';
}
