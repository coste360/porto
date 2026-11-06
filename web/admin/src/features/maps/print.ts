// Print layout of the map page (spec 8.7): paper sizes in mm, scale 1:N, the part of the map that fits.
export type Paper = 'A4' | 'A3';
export type Orientation = 'portrait' | 'landscape';
export type PrintScale = 500 | 1000 | 2000 | 'fit';

const PAPER_MM: Record<Paper, [number, number]> = { A4: [210, 297], A3: [297, 420] };
/** Margins plus header and legend space (mm). */
const MARGIN_MM = 12;
const CHROME_MM = 30;

export type PrintLayout = {
  denominator: number;
  mapWmm: number;
  mapHmm: number;
  viewWcm: number;
  viewHcm: number;
  cropped: boolean;
  bar: { mm: number; cm: number };
};

/** Nice length (cm) of a printed scale bar at most `maxMm` long at 1:N. */
export function printScaleBar(denominator: number, maxMm = 50): { mm: number; cm: number } {
  const maxCm = (maxMm * denominator) / 10;
  let best = 100;
  for (let exp = 2; exp < 9; exp++) {
    for (const n of [1, 2, 5]) {
      const v = n * 10 ** exp;
      if (v <= maxCm) best = v;
    }
  }
  return { mm: (best * 10) / denominator, cm: best };
}

export function printLayout(
  widthCm: number,
  heightCm: number,
  paper: Paper,
  orientation: Orientation,
  scale: PrintScale,
): PrintLayout {
  const [short, long] = PAPER_MM[paper];
  const [pw, ph] = orientation === 'portrait' ? [short, long] : [long, short];
  const availW = pw - 2 * MARGIN_MM;
  const availH = ph - 2 * MARGIN_MM - CHROME_MM;
  // 1 mm on paper = N/10 cm on the ground.
  const fitDen = Math.ceil(Math.max((widthCm * 10) / availW, (heightCm * 10) / availH));
  const denominator = scale === 'fit' ? Math.max(1, fitDen) : scale;
  const mapWmm = Math.min(availW, (widthCm * 10) / denominator);
  const mapHmm = Math.min(availH, (heightCm * 10) / denominator);
  return {
    denominator,
    mapWmm,
    mapHmm,
    viewWcm: (mapWmm * denominator) / 10,
    viewHcm: (mapHmm * denominator) / 10,
    cropped: scale !== 'fit' && denominator < fitDen,
    bar: printScaleBar(denominator),
  };
}
