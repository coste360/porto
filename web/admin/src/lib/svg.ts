/** Server-rendered QR SVG as an <img> source (never injected as HTML). */
export function svgDataUrl(svg: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}
