import { CalendarDaysIcon, PlusIcon, QrCodeIcon, SailboatIcon, WalletIcon } from 'lucide-react';
import { useState } from 'react';

import { useRentalDay, type RentalDayEntry } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { useQueue } from '@porto/shared/offline';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { EmptyState } from '@porto/shared/ui/patterns';
import { Tabs, TabsContent } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { canUseRentalPhone } from '../../../lib/landing';
import { QuickCashDialog } from '../../rental-handover/components/QuickCashDialog';
import { parseRentalQr } from '../../rental-handover/lib/wizard';
import { PhoneTabBar } from '../components/PhoneTabBar';
import { CheckInWizard } from './CheckInWizard';
import { CheckOutWizard } from './CheckOutWizard';
import { WalkInTab } from './WalkInTab';

type View = { kind: 'list' } | { kind: 'checkout'; id: string } | { kind: 'checkin'; id: string };
type Tab = 'today' | 'new' | 'scan' | 'cash';

const SECTIONS = ['pickups', 'out', 'returns_due', 'late'] as const;

/** Rental phone screen (m11-plan §14.3): Oggi (departures, at sea, returns, late), Nuovo (walk-in booking from
 * now, online), Scan (PR1: QR), Cassa (open rental charges); the check-out and check-in wizards open from a
 * booking. */
export function RentalPhonePage() {
  const { t, i18n } = useT();
  const { marina, marinaId } = useCurrentMarina();
  const day = useRentalDay(marinaId);
  const queue = useQueue(marinaId);
  const [view, setView] = useState<View>({ kind: 'list' });
  const [tab, setTab] = useState<Tab>('today');
  const [code, setCode] = useState('');
  const [cashFor, setCashFor] = useState<string | null>(null);
  const back = () => setView({ kind: 'list' });
  const open = (e: RentalDayEntry) => {
    if (e.status === 'confirmed') setView({ kind: 'checkout', id: e.booking_id });
    else if (e.status === 'checked_out') setView({ kind: 'checkin', id: e.booking_id });
    else setCashFor(e.booking_id);
  };
  const all = SECTIONS.flatMap((s) => day.data?.[s] ?? []);
  const unpaid = all.filter(
    (e, i) =>
      (e.unpaid_cents ?? 0) > 0 && all.findIndex((x) => x.booking_id === e.booking_id) === i,
  );
  const scanned = parseRentalQr(code);
  return (
    <RequirePermission allowed={canUseRentalPhone(marina?.permissions ?? [])}>
      <div data-testid="rental-phone" className="flex flex-col gap-4 p-4 pb-20">
        {view.kind === 'checkout' ? (
          <CheckOutWizard bookingId={view.id} onDone={back} />
        ) : view.kind === 'checkin' ? (
          <CheckInWizard bookingId={view.id} onDone={back} />
        ) : (
          <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
            <h1 className="text-xl font-semibold">{t('rentalHandover:title')}</h1>
            {queue.pending > 0 ? (
              <p role="status" className="text-muted-foreground text-sm">
                {t('rentalHandover:queued', { count: queue.pending })}
              </p>
            ) : null}
            <TabsContent value="today" className="flex flex-col gap-4">
              {all.length === 0 ? (
                <EmptyState icon={<SailboatIcon />} title={t('rentalHandover:today.empty')} />
              ) : (
                SECTIONS.map((s) =>
                  (day.data?.[s].length ?? 0) > 0 ? (
                    <section
                      key={s}
                      className="flex flex-col gap-2"
                      data-testid={`rental-day-${s}`}
                    >
                      <h2 className="font-medium">{t(`rentalHandover:today.${s}`)}</h2>
                      {day.data![s].map((e) => (
                        <button
                          key={`${s}-${e.booking_id}`}
                          type="button"
                          data-testid="rental-day-entry"
                          className="hover:bg-muted flex min-h-11 w-full flex-col items-start gap-1 rounded-md border p-3 text-left"
                          onClick={() => open(e)}
                        >
                          <span className="flex w-full items-center justify-between gap-2">
                            <span className="font-medium">
                              {e.unit_code} · {e.number}
                            </span>
                            {e.late ? (
                              <Badge variant="destructive">
                                {t('rentalHandover:today.lateBadge')}
                              </Badge>
                            ) : null}
                          </span>
                          <span className="text-muted-foreground text-sm">
                            {e.renter_name ?? ''}{' '}
                            {new Date(e.starts_at).toLocaleTimeString(i18n.language, {
                              hour: '2-digit',
                              minute: '2-digit',
                            })}
                            –
                            {new Date(e.ends_at).toLocaleTimeString(i18n.language, {
                              hour: '2-digit',
                              minute: '2-digit',
                            })}
                          </span>
                        </button>
                      ))}
                    </section>
                  ) : null,
                )
              )}
            </TabsContent>
            <TabsContent value="new" className="flex flex-col gap-3">
              <WalkInTab
                marinaId={marinaId}
                onCreated={(id) => setView({ kind: 'checkout', id })}
              />
            </TabsContent>
            <TabsContent value="scan" className="flex flex-col gap-3">
              <Field>
                <FieldLabel htmlFor="rental-scan">{t('rentalHandover:scan.label')}</FieldLabel>
                <Input
                  id="rental-scan"
                  autoComplete="off"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                />
              </Field>
              <Button
                className="min-h-11"
                disabled={!scanned}
                onClick={() => {
                  const hit = all.find((e) => e.booking_id === scanned);
                  if (hit) open(hit);
                  else if (scanned) setView({ kind: 'checkout', id: scanned });
                }}
              >
                {t('rentalHandover:scan.open')}
              </Button>
            </TabsContent>
            <TabsContent value="cash" className="flex flex-col gap-2">
              {unpaid.length === 0 ? (
                <EmptyState icon={<WalletIcon />} title={t('rentalHandover:cash.empty')} />
              ) : (
                unpaid.map((e) => (
                  <Button
                    key={e.booking_id}
                    variant="outline"
                    className="min-h-11 justify-between"
                    onClick={() => setCashFor(e.booking_id)}
                  >
                    <span>
                      {e.unit_code} · {e.number}
                    </span>
                    <span>{formatCents(e.unpaid_cents ?? 0, 'EUR', i18n.language)}</span>
                  </Button>
                ))
              )}
            </TabsContent>
            <PhoneTabBar<Tab>
              tabs={[
                {
                  value: 'today',
                  label: t('rentalHandover:tabs.today'),
                  icon: <CalendarDaysIcon />,
                },
                { value: 'new', label: t('rentalHandover:tabs.new'), icon: <PlusIcon /> },
                { value: 'scan', label: t('rentalHandover:tabs.scan'), icon: <QrCodeIcon /> },
                { value: 'cash', label: t('rentalHandover:tabs.cash'), icon: <WalletIcon /> },
              ]}
            />
          </Tabs>
        )}
        {cashFor ? (
          <QuickCashDialog
            marinaId={marinaId}
            bookingId={cashFor}
            open
            onOpenChange={(v) => !v && setCashFor(null)}
          />
        ) : null}
      </div>
    </RequirePermission>
  );
}
