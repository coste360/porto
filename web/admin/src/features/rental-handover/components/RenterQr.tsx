import { svgDataUrl } from '../../../lib/svg';

/**
 * A server-rendered QR (internal/qrsvg) of a page the renter opens on their own phone: the deposit
 * pre-authorisation or the quick-cash payment link (m11-plan §7 step 3, §9.3). Shown as an image, never as HTML;
 * the plain link stays next to it for the operator.
 */
export function RenterQr({
  svg,
  alt,
  hint,
  testId,
}: {
  svg: string;
  alt: string;
  hint: string;
  testId: string;
}) {
  return (
    <figure className="flex flex-col items-center gap-2">
      <img
        src={svgDataUrl(svg)}
        alt={alt}
        className="bg-brand-white size-56 max-w-full rounded-md border p-2"
        data-testid={testId}
      />
      <figcaption className="text-muted-foreground text-center text-sm">{hint}</figcaption>
    </figure>
  );
}
