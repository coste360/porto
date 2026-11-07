import { Link } from '@tanstack/react-router';
import { XIcon } from 'lucide-react';
import type { ReactNode } from 'react';

import type { DayElementData } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/map';
import type { DaySubject } from '@porto/shared/map';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@porto/shared/ui/card';
import { formatDateTime, formatIsoDate } from '@porto/shared/lib/format';

import type { QuickAction } from '../lib/quickActions';

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex justify-between gap-2 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="text-right">{children}</dd>
    </div>
  );
}

/** Quick card of a berth, a resource or a vessel (spec 9.3): data and actions by permission. */
export function QuickCard({
  marinaId,
  subject,
  element,
  actions,
  onAction,
  onClose,
}: {
  marinaId: string;
  subject: DaySubject;
  element?: DayElementData;
  actions: readonly QuickAction[];
  onAction: (a: QuickAction) => void;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const s = subject.subject;
  const lang = i18n.language;
  return (
    <Card data-testid="ops-quick-card" aria-label={t('opsMap:quick.label', { code: subject.code })}>
      <CardHeader>
        <CardTitle>{s.vessel?.name ?? subject.code}</CardTitle>
        <CardDescription>
          {t(`occupancyEnums:resourceType.${subject.resourceType}`)} {subject.code}
        </CardDescription>
        <CardAction>
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('common:actions.close')}
            onClick={onClose}
          >
            <XIcon />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex flex-wrap gap-1">
          <Badge variant="secondary">{t(`occupancyEnums:dayStatus.${s.status}`)}</Badge>
          {s.anomalies.map((a) => (
            <Badge key={a} variant="destructive">
              {t(`occupancyEnums:anomaly.${a}`)}
            </Badge>
          ))}
        </div>
        <dl className="flex flex-col gap-1">
          {s.vessel ? (
            <Row label={t('opsMap:quick.size')}>
              {formatCm(s.vessel.loa_cm, lang)} × {formatCm(s.vessel.beam_cm, lang)}
            </Row>
          ) : null}
          {s.vessel ? (
            <Row label={t('opsMap:quick.hull')}>
              {t(`occupancyEnums:lens.legend.${s.vessel.hull_type}`)}
            </Row>
          ) : null}
          {s.customer?.display_name ? (
            <Row label={t('opsMap:quick.customer')}>
              <Link
                to="/m/$marinaId/customers/$customerId"
                params={{ marinaId, customerId: s.customer.id }}
                className="underline"
              >
                {s.customer.display_name}
              </Link>
            </Row>
          ) : null}
          {s.contract ? (
            <Row label={t('opsMap:quick.contract')}>
              {s.contract.kind === 'transit' ? (
                <Link
                  to="/m/$marinaId/transits/$contractId"
                  params={{ marinaId, contractId: s.contract.id }}
                  className="underline"
                >
                  {s.contract.code}
                </Link>
              ) : (
                <Link
                  to="/m/$marinaId/contracts/$contractId"
                  params={{ marinaId, contractId: s.contract.id }}
                  className="underline"
                >
                  {s.contract.code}
                </Link>
              )}{' '}
              ·{' '}
              {t(
                `occupancyEnums:${s.contract.kind === 'transit' ? 'transitStatus' : 'contractStatus'}.${s.contract.status}`,
              )}
            </Row>
          ) : null}
          {s.presence ? (
            <Row label={t('opsMap:quick.presence')}>
              {t(`occupancyEnums:presenceState.${s.presence.state}`)} ·{' '}
              {formatDateTime(s.presence.since, lang)}
            </Row>
          ) : null}
          {s.presence?.expected_return_on ? (
            <Row label={t('opsMap:quick.expectedReturn')}>
              {formatIsoDate(s.presence.expected_return_on)}
            </Row>
          ) : null}
          {s.release ? (
            <Row label={t('opsMap:quick.release')}>
              {formatIsoDate(s.release.from)} – {formatIsoDate(s.release.to)}
              {s.release.resold ? ` · ${t('opsMap:quick.resold')}` : ''}
            </Row>
          ) : null}
          {element?.hold_expires_at ? (
            <Row label={t('opsMap:quick.holdExpires')}>
              {formatDateTime(element.hold_expires_at, lang)}
            </Row>
          ) : null}
          {s.fit ? (
            <Row label={t('opsMap:quick.fill')}>
              {Math.floor(s.fit.fill_bp / 100)}%
              {s.fit.override ? ` · ${t('opsMap:quick.override')}` : ''}
            </Row>
          ) : null}
          {s.fit?.blocking_codes.map((c) => (
            <Row key={c} label={t('opsMap:quick.fitIssue')}>
              {t(`occupancyEnums:fit.${c}`)}
            </Row>
          ))}
          {s.deadlines?.contract_ends_on ? (
            <Row label={t('opsMap:quick.contractEnds')}>
              {formatIsoDate(s.deadlines.contract_ends_on)}
            </Row>
          ) : null}
          {s.deadlines ? (
            <Row label={t('opsMap:quick.insurance')}>
              {t(`occupancyEnums:deadlineState.${s.deadlines.insurance}`)}
            </Row>
          ) : null}
        </dl>
        {actions.length > 0 ? (
          <div
            className="flex flex-wrap gap-2"
            role="group"
            aria-label={t('common:actions.actions')}
          >
            {actions.map((a) => (
              <Button
                key={a}
                size="sm"
                variant={a === 'undo' ? 'secondary' : 'outline'}
                data-testid={`ops-quick-action-${a}`}
                onClick={() => onAction(a)}
              >
                {t(`opsMap:quick.action.${a}`)}
              </Button>
            ))}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
