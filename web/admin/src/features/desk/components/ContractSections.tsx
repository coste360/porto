import { Link } from '@tanstack/react-router';
import { MapPinIcon } from 'lucide-react';
import { useState } from 'react';

import { useRevokeRelease, type Contract } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  formatCents,
  formatDateTime,
  formatIsoDate,
  formatQtyMilli,
} from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
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

import { useCurrentMarina } from '../../../auth/hooks';
import { opsMapSearch } from '../../operations/public';
import { DeskProblemAlert } from './DeskProblemAlert';

/** "Mostra sulla mappa": the operational view focused on the current resource of the contract. */
export function ShowOnMapButton({ contract, date }: { contract: Contract; date: string }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const current =
    contract.allocations.find(
      (a) => a.status === 'active' && a.first_night <= date && a.last_night >= date,
    ) ?? contract.allocations.find((a) => a.status === 'active');
  if (!current) return null;
  return (
    <Button variant="outline" asChild>
      <Link
        to="/m/$marinaId/ops"
        params={{ marinaId }}
        search={opsMapSearch({ mapId: current.map_id, date, focus: current.resource_id })}
      >
        <MapPinIcon />
        {t('desk:contract.showOnMap')}
      </Link>
    </Button>
  );
}

export function LinesCard({ contract }: { contract: Contract }) {
  const { t, i18n } = useT();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('desk:contract.lines')}</CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('desk:columns.line')}</TableHead>
              <TableHead>{t('desk:columns.qty')}</TableHead>
              <TableHead className="text-right">{t('desk:columns.amount')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {contract.lines.map((l) => (
              <TableRow key={l.id}>
                <TableCell>
                  {l.line_kind === 'resource'
                    ? (l.resource_code ??
                      t(`occupancyEnums:resourceType.${l.resource_type ?? 'berth'}`))
                    : t('desk:contract.serviceLine')}
                </TableCell>
                <TableCell className="tabular-nums">
                  {formatQtyMilli(l.qty_milli, i18n.language)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatCents(l.amount_cents, 'EUR', i18n.language)}
                </TableCell>
              </TableRow>
            ))}
            <TableRow>
              <TableCell colSpan={2} className="font-medium">
                {t('desk:columns.total')}
              </TableCell>
              <TableCell
                className="text-right font-medium tabular-nums"
                data-testid="desk-contract-total"
              >
                {formatCents(contract.total_cents, 'EUR', i18n.language)}
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

/** Allocations timeline: resource, nights, kind and status, in order; moved allocations show their origin. */
export function AllocationsCard({ contract }: { contract: Contract }) {
  const { t } = useT();
  const sorted = [...contract.allocations].sort((a, b) =>
    a.first_night.localeCompare(b.first_night),
  );
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('desk:contract.allocations')}</CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('desk:columns.resource')}</TableHead>
              <TableHead>{t('desk:columns.nights')}</TableHead>
              <TableHead>{t('desk:columns.kind')}</TableHead>
              <TableHead>{t('desk:columns.status')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sorted.map((a) => (
              <TableRow key={a.id} data-testid="desk-allocation-row">
                <TableCell>
                  {a.resource_code ?? a.resource_id}
                  {a.moved_from_id ? (
                    <span className="text-muted-foreground"> {t('desk:contract.moved')}</span>
                  ) : null}
                </TableCell>
                <TableCell>{`${formatIsoDate(a.first_night)} – ${formatIsoDate(a.last_night)}`}</TableCell>
                <TableCell>{t(`occupancyEnums:allocationKind.${a.kind}`)}</TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    <Badge variant="secondary">
                      {t(`occupancyEnums:allocationStatus.${a.status}`)}
                    </Badge>
                    {a.fit_override ? (
                      <Badge variant="outline">{t('desk:contract.fitOverride')}</Badge>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

export function ReleasesCard({ contract, canRevoke }: { contract: Contract; canRevoke: boolean }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const revoke = useRevokeRelease(marinaId);
  const [revoking, setRevoking] = useState<string | null>(null);
  if (contract.releases.length === 0) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('desk:contract.releases')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('desk:columns.nights')}</TableHead>
              <TableHead>{t('desk:release.source')}</TableHead>
              <TableHead>{t('desk:columns.status')}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {contract.releases.map((r) => (
              <TableRow key={r.id}>
                <TableCell>{`${formatIsoDate(r.from_night)} – ${formatIsoDate(r.to_night)}`}</TableCell>
                <TableCell>{t(`occupancyEnums:releaseSource.${r.source}`)}</TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    <Badge variant="secondary">
                      {t(`occupancyEnums:releaseStatus.${r.status}`)}
                    </Badge>
                    {r.resold_nights > 0 ? (
                      <Badge variant="outline">
                        {t('desk:release.resold', { count: r.resold_nights })}
                      </Badge>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell className="text-right">
                  {canRevoke && r.status === 'active' ? (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={revoke.isPending}
                      onClick={() => {
                        setRevoking(r.id);
                        revoke.mutate({ id: r.id, version: r.version, body: {} });
                      }}
                    >
                      {t('desk:release.revoke')}
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {revoking ? <DeskProblemAlert error={revoke.error} /> : null}
      </CardContent>
    </Card>
  );
}

export function PresencesCard({ contract }: { contract: Contract }) {
  const { t, i18n } = useT();
  if (contract.presences.length === 0) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('desk:contract.presences')}</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="flex flex-col gap-1 text-sm">
          {contract.presences.map((p) => (
            <li key={p.id} className="flex flex-wrap gap-2">
              <Badge variant="secondary">{t(`occupancyEnums:presenceEvent.${p.event}`)}</Badge>
              <span>{formatDateTime(p.at, i18n.language)}</span>
              {p.notes ? <span className="text-muted-foreground">{p.notes}</span> : null}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}
