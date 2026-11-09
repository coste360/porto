import {
  AnchorIcon,
  BathIcon,
  CarIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  DropletsIcon,
  FuelIcon,
  GlobeIcon,
  MailIcon,
  PhoneIcon,
  PlugIcon,
  RadioIcon,
  ShipIcon,
  ShoppingBagIcon,
  ShowerHeadIcon,
  TrashIcon,
  UtensilsIcon,
  WashingMachineIcon,
  WifiIcon,
  WrenchIcon,
  BusIcon,
  WarehouseIcon,
  SailboatIcon,
  ForkliftIcon,
  type LucideIcon,
} from 'lucide-react';
import { useState } from 'react';

import type {
  PublicOccupancy,
  PublicPriceMatrix,
  PublicProfile,
  ProfileServices,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatCm, formatDateTime } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { ShelterRose } from './ShelterRose';

type Photo = NonNullable<PublicProfile['photos']>[number];

/** A titled section of the page (landmark with its heading). */
export function Section({
  id,
  title,
  children,
}: {
  id: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section aria-labelledby={`${id}-title`} data-testid={`profile-section-${id}`}>
      <Card>
        <CardHeader>
          <CardTitle>
            <h2 id={`${id}-title`} className="text-lg font-semibold">
              {title}
            </h2>
          </CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">{children}</CardContent>
      </Card>
    </section>
  );
}

/** Photos with previous/next buttons (no carousel dependency). */
export function Photos({ photos, srcOf }: { photos: Photo[]; srcOf: (url: string) => string }) {
  const { t } = useT();
  const [i, setI] = useState(0);
  if (photos.length === 0) return null;
  const n = photos.length;
  const p = photos[Math.min(i, n - 1)] as Photo;
  return (
    <figure
      className="flex flex-col gap-2"
      aria-roledescription="carousel"
      data-testid="profile-photos"
    >
      <img src={srcOf(p.url)} alt={p.alt} className="aspect-video w-full rounded-md object-cover" />
      {n > 1 ? (
        <div className="flex items-center justify-between">
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label={t('publicProfile:previousPhoto')}
            onClick={() => setI((i - 1 + n) % n)}
          >
            <ChevronLeftIcon />
          </Button>
          <figcaption className="text-muted-foreground text-sm" aria-live="polite">
            {`${Math.min(i, n - 1) + 1} / ${n}`}
          </figcaption>
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label={t('publicProfile:nextPhoto')}
            onClick={() => setI((i + 1) % n)}
          >
            <ChevronRightIcon />
          </Button>
        </div>
      ) : null}
    </figure>
  );
}

export function Contacts({ p }: { p: PublicProfile }) {
  const { t } = useT();
  const addr = p.address;
  const line = [
    addr?.street,
    [addr?.postal_code, addr?.city, addr?.province].filter(Boolean).join(' '),
    addr?.country,
  ]
    .filter((x) => x && x.trim() !== '')
    .join(', ');
  return (
    <dl className="grid gap-3 sm:grid-cols-2">
      {p.phone ? (
        <div>
          <dt className="text-muted-foreground text-sm">{t('publicProfile:phone')}</dt>
          <dd>
            <a
              className="text-primary inline-flex items-center gap-2 underline-offset-4 hover:underline"
              href={`tel:${p.phone.replace(/\s+/g, '')}`}
            >
              <PhoneIcon className="size-4" />
              {p.phone}
            </a>
          </dd>
        </div>
      ) : null}
      {p.email ? (
        <div>
          <dt className="text-muted-foreground text-sm">{t('publicProfile:email')}</dt>
          <dd>
            <a
              className="text-primary inline-flex items-center gap-2 underline-offset-4 hover:underline"
              href={`mailto:${p.email}`}
            >
              <MailIcon className="size-4" />
              {p.email}
            </a>
          </dd>
        </div>
      ) : null}
      {p.website ? (
        <div>
          <dt className="text-muted-foreground text-sm">{t('publicProfile:website')}</dt>
          <dd>
            <a
              className="text-primary inline-flex items-center gap-2 break-all underline-offset-4 hover:underline"
              href={p.website}
              rel="noopener noreferrer"
            >
              <GlobeIcon className="size-4" />
              {p.website.replace(/^https:\/\//, '')}
            </a>
          </dd>
        </div>
      ) : null}
      {(p.vhf_channels ?? []).length > 0 ? (
        <div>
          <dt className="text-muted-foreground text-sm">{t('publicProfile:vhf')}</dt>
          <dd className="inline-flex items-center gap-2">
            <RadioIcon className="size-4" />
            {(p.vhf_channels ?? []).join(', ')}
          </dd>
        </div>
      ) : null}
      {line ? (
        <div>
          <dt className="text-muted-foreground text-sm">{t('publicProfile:address')}</dt>
          <dd>{line}</dd>
        </div>
      ) : null}
      {p.latitude && p.longitude ? (
        <div>
          <dt className="text-muted-foreground text-sm">{t('publicProfile:position')}</dt>
          <dd className="font-mono text-sm">{`${p.latitude}, ${p.longitude}`}</dd>
        </div>
      ) : null}
    </dl>
  );
}

function HoursTable({
  caption,
  hours,
}: {
  caption: string;
  hours: NonNullable<PublicProfile['office_hours']>;
}) {
  const { t } = useT();
  if (hours.length === 0) return null;
  return (
    <Table>
      <caption className="text-left font-medium">{caption}</caption>
      <TableBody>
        {hours.map((h, i) => (
          <TableRow key={`${h.weekday}-${h.from}-${i}`}>
            <TableHead scope="row">{t(`publicProfile:weekday.${h.weekday}`)}</TableHead>
            <TableCell>{`${h.from}–${h.to}`}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function Hours({ p }: { p: PublicProfile }) {
  const { t } = useT();
  return (
    <>
      <HoursTable caption={t('publicProfile:officeHours')} hours={p.office_hours ?? []} />
      <HoursTable caption={t('publicProfile:dockHours')} hours={p.dock_hours ?? []} />
      {p.hours_note ? <p className="text-muted-foreground text-sm">{p.hours_note}</p> : null}
    </>
  );
}

function metres(cm: number, locale: string): string {
  return formatCm(cm, locale);
}

export function Features({ p }: { p: PublicProfile }) {
  const { t, i18n } = useT();
  const rows: [string, string][] = [];
  if (p.berths_total !== undefined)
    rows.push([t('publicProfile:berthsTotal'), String(p.berths_total)]);
  if (p.transit_berths !== undefined)
    rows.push([t('publicProfile:transitBerths'), String(p.transit_berths)]);
  if (p.max_loa_cm !== undefined)
    rows.push([t('publicProfile:maxLoa'), metres(p.max_loa_cm, i18n.language)]);
  if (p.depth_entrance_cm !== undefined)
    rows.push([t('publicProfile:depthEntrance'), metres(p.depth_entrance_cm, i18n.language)]);
  if (p.depth_quay_cm !== undefined)
    rows.push([t('publicProfile:depthQuay'), metres(p.depth_quay_cm, i18n.language)]);
  if (p.depth_min_cm !== undefined && p.depth_max_cm !== undefined) {
    rows.push([
      t('publicProfile:depthRange'),
      `${metres(p.depth_min_cm, i18n.language)} – ${metres(p.depth_max_cm, i18n.language)}`,
    ]);
  }
  if (p.seabed) rows.push([t('publicProfile:seabedLabel'), t(`publicProfile:seabed.${p.seabed}`)]);
  return (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <dl className="grid flex-1 grid-cols-2 gap-x-4 gap-y-2">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted-foreground text-sm">{k}</dt>
            <dd className="font-medium">{v}</dd>
          </div>
        ))}
      </dl>
      {(p.shelter ?? []).length > 0 ? (
        <div className="flex flex-col items-center gap-1">
          <ShelterRose shelter={p.shelter ?? []} />
          <span className="text-muted-foreground text-sm">{t('publicProfile:shelter')}</span>
        </div>
      ) : null}
    </div>
  );
}

const FLAG_ICONS: [keyof ProfileServices & string, LucideIcon][] = [
  ['water', DropletsIcon],
  ['wifi', WifiIcon],
  ['toilets', BathIcon],
  ['showers', ShowerHeadIcon],
  ['laundry', WashingMachineIcon],
  ['pump_out', AnchorIcon],
  ['waste', TrashIcon],
  ['slipway', ShipIcon],
  ['yard', WrenchIcon],
  ['storage', WarehouseIcon],
  ['rental', SailboatIcon],
  ['parking', CarIcon],
  ['restaurant', UtensilsIcon],
  ['shops', ShoppingBagIcon],
  ['transport', BusIcon],
];

/** The services the profile offers, as an icon grid (nothing when none). */
export function Services({ p }: { p: PublicProfile }) {
  const { t } = useT();
  const s = p.services ?? {};
  const items: { key: string; icon: LucideIcon; label: string }[] = [];
  if ((s.power_amps ?? []).length > 0) {
    items.push({
      key: 'power',
      icon: PlugIcon,
      label: t('publicProfile:powerAmps', { amps: (s.power_amps ?? []).join('/') }),
    });
  }
  if (s.fuel?.petrol || s.fuel?.diesel) {
    const kinds = [
      s.fuel.petrol ? t('publicProfile:fuelPetrol') : '',
      s.fuel.diesel ? t('publicProfile:fuelDiesel') : '',
    ]
      .filter(Boolean)
      .join(', ');
    items.push({
      key: 'fuel',
      icon: FuelIcon,
      label: `${t('publicProfile:services.fuel')}: ${kinds}${p.fuel_hours ? ` (${p.fuel_hours})` : ''}`,
    });
  }
  for (const [key, icon] of FLAG_ICONS) {
    if (s[key] === true) items.push({ key, icon, label: t(`publicProfile:services.${key}`) });
  }
  if (s.travel_lift_kg) {
    items.push({
      key: 'travel_lift',
      icon: ForkliftIcon,
      label: t('publicProfile:liftTonnes', {
        label: t('publicProfile:services.travel_lift'),
        t: Math.floor(s.travel_lift_kg / 1000),
      }),
    });
  }
  if (s.crane_kg) {
    items.push({
      key: 'crane',
      icon: ForkliftIcon,
      label: t('publicProfile:liftTonnes', {
        label: t('publicProfile:services.crane'),
        t: Math.floor(s.crane_kg / 1000),
      }),
    });
  }
  if (items.length === 0) return null;
  return (
    <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2" data-testid="profile-services">
      {items.map(({ key, icon: Icon, label }) => (
        <li key={key} className="flex items-center gap-2">
          <Icon className="text-primary size-4 shrink-0" aria-hidden="true" />
          <span>{label}</span>
        </li>
      ))}
    </ul>
  );
}

/** The transit price matrix: rows bands or categories, columns periods, totals VAT included. */
export function Prices({ m }: { m: PublicPriceMatrix }) {
  const { t, i18n } = useT();
  const cell = (r: number, c: number) => m.cells.find((x) => x.row === r && x.col === c);
  return (
    <div className="flex flex-col gap-2 overflow-x-auto">
      <Table data-testid="profile-prices">
        <TableHeader>
          <TableRow>
            <TableHead scope="col">{t('publicProfile:pricesRow')}</TableHead>
            {m.columns.map((c) => (
              <TableHead key={c.key} scope="col">
                {c.label}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {m.rows.map((r, ri) => (
            <TableRow key={r.key}>
              <TableHead scope="row">
                <span className="flex flex-col">
                  <span>{r.label}</span>
                  {r.max_loa_cm ? (
                    <span className="text-muted-foreground text-xs">
                      {t('publicProfile:upTo', {
                        m: formatCm(r.max_loa_cm, i18n.language).replace(/\s*m$/, ''),
                      })}
                    </span>
                  ) : null}
                </span>
              </TableHead>
              {m.columns.map((c, ci) => {
                const x = cell(ri, ci);
                return (
                  <TableCell key={c.key} className="tabular-nums">
                    {x?.total_cents !== undefined
                      ? formatCents(x.total_cents, m.currency, i18n.language)
                      : t('publicProfile:noPrice')}
                  </TableCell>
                );
              })}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <p className="text-muted-foreground text-sm">
        {m.price_includes_vat
          ? t('publicProfile:pricesVatIncluded')
          : t('publicProfile:pricesVatExcluded')}
      </p>
    </div>
  );
}

/** Free berths per category tonight: counts only. */
export function Occupancy({ o }: { o: PublicOccupancy }) {
  const { t, i18n } = useT();
  const name = (n: PublicOccupancy['categories'][number]['name']) =>
    i18n.language === 'en' && n.en ? n.en : n.it;
  return (
    <>
      <ul className="flex flex-col gap-2" data-testid="profile-occupancy">
        {o.categories.map((c) => (
          <li key={c.code} className="flex items-center justify-between gap-4">
            <span>{name(c.name)}</span>
            <span className="font-medium tabular-nums">
              {t('publicProfile:occupancyFree', { free: c.free_berths, total: c.total_berths })}
            </span>
          </li>
        ))}
      </ul>
      <p className="text-muted-foreground text-sm">
        {t('publicProfile:occupancyAsOf', { time: formatDateTime(o.as_of, i18n.language) })}
      </p>
    </>
  );
}
