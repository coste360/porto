import { CheckCircle2Icon, CircleAlertIcon, PrinterIcon, UnlinkIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import type { DesktopStatus, LanStatus } from '@porto/shared/api';
import {
  useAppWindowStatus,
  useBridgeStatus,
  useDesktopPeripherals,
  useSetDesktopKiosk,
  useTestBridgePeripheral,
  useUnpairDesktopRemote,
  useUpdateDesktopPeripherals,
  type PeripheralSettings,
} from '@porto/shared/desktop/hooks';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@porto/shared/ui/card';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ConfirmDialog, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';

import { LoadingState, QueryErrorState } from '../../components/PageStates';
import { toastProblem } from '../../lib/toast';
import { PinDialog } from './PinDialog';

// M6b cards of the desktop settings page (m6b-plan §7, §8): peripherals, kiosk, the online instance of the remote
// mode, and the CA download of the local network. In remote mode every change asks for the app PIN.

type Device = 'receipt' | 'label' | 'fiscal';

/** Runs `action` directly (local mode, owner session) or after asking for the app PIN (remote mode). */
function usePinGate(remote: boolean) {
  const [pending, setPending] = useState<((pin?: string) => void) | null>(null);
  return {
    run: (action: (pin?: string) => void) => (remote ? setPending(() => action) : action()),
    dialogProps: {
      open: pending !== null,
      onOpenChange: (o: boolean) => {
        if (!o) setPending(null);
      },
      onConfirm: (pin: string) => {
        pending?.(pin);
        setPending(null);
      },
    },
  };
}

export function PeripheralsCard() {
  const { t } = useT();
  const win = useAppWindowStatus();
  const settings = useDesktopPeripherals(win.loopback);
  if (settings.isPending) return <LoadingState rows={3} />;
  if (settings.isError)
    return <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />;
  return (
    <Card data-testid="desktop-peripherals-card">
      <CardHeader>
        <CardTitle>{t('desktop:peripherals.title')}</CardTitle>
        <CardDescription>{t('desktop:peripherals.description')}</CardDescription>
      </CardHeader>
      <PeripheralsForm
        key={JSON.stringify(settings.data)}
        initial={settings.data}
        remote={win.remote}
      />
    </Card>
  );
}

function DriverSelect({
  id,
  value,
  options,
  labels,
  onChange,
}: {
  id: string;
  value: string;
  options: readonly string[];
  labels: string;
  onChange: (v: string) => void;
}) {
  const { t } = useT();
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o} value={o}>
            {t(`desktop:${labels}.${o}`)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function ReadyBadge({ ready }: { ready: boolean | undefined }) {
  const { t } = useT();
  if (ready === undefined) return null;
  return (
    <Badge variant={ready ? 'secondary' : 'outline'}>
      {ready ? <CheckCircle2Icon /> : <CircleAlertIcon />}
      {ready ? t('desktop:peripherals.ready') : t('desktop:peripherals.notReady')}
    </Badge>
  );
}

function PeripheralsForm({ initial, remote }: { initial: PeripheralSettings; remote: boolean }) {
  const { t } = useT();
  const [s, setS] = useState<PeripheralSettings>(initial);
  const update = useUpdateDesktopPeripherals();
  const test = useTestBridgePeripheral();
  const bridge = useBridgeStatus(true);
  const gate = usePinGate(remote);
  const dirty = JSON.stringify(s) !== JSON.stringify(initial);

  const save = () =>
    gate.run((pin) =>
      update.mutate(
        { settings: s, pin },
        {
          onSuccess: () => toast.success(t('desktop:peripherals.saved')),
        },
      ),
    );
  const runTest = (device: Device) =>
    gate.run((pin) =>
      test.mutate(
        { device, pin },
        {
          onSuccess: () => toast.success(t('desktop:peripherals.testDone')),
          onError: (e) => toastProblem(t, e),
        },
      ),
    );
  const testButton = (device: Device, enabled: boolean) => (
    <Button
      type="button"
      variant="outline"
      size="sm"
      disabled={!enabled || dirty || test.isPending}
      onClick={() => runTest(device)}
    >
      <PrinterIcon />
      {t('desktop:peripherals.test')}
    </Button>
  );

  return (
    <>
      <CardContent className="flex flex-col gap-6">
        {update.error ? <ProblemAlert error={update.error} /> : null}
        <FieldGroup>
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium">{t('desktop:peripherals.receipt')}</span>
            <div className="flex items-center gap-2">
              <ReadyBadge ready={bridge.data?.receipt.ready} />
              {testButton('receipt', s.receipt.driver !== 'none')}
            </div>
          </div>
          <Field>
            <FieldLabel htmlFor="periph-receipt-driver">
              {t('desktop:peripherals.driver')}
            </FieldLabel>
            <DriverSelect
              id="periph-receipt-driver"
              value={s.receipt.driver}
              options={['none', 'fake', 'escpos'] as const}
              labels="peripherals.drivers"
              onChange={(v) =>
                setS({
                  ...s,
                  receipt: { ...s.receipt, driver: v as PeripheralSettings['receipt']['driver'] },
                })
              }
            />
          </Field>
          {s.receipt.driver === 'escpos' ? (
            <Field>
              <FieldLabel htmlFor="periph-receipt-target">
                {t('desktop:peripherals.target')}
              </FieldLabel>
              <Input
                id="periph-receipt-target"
                className="font-mono"
                value={s.receipt.target ?? ''}
                onChange={(e) => setS({ ...s, receipt: { ...s.receipt, target: e.target.value } })}
              />
              <FieldDescription>{t('desktop:peripherals.targetHint')}</FieldDescription>
            </Field>
          ) : null}
          {s.receipt.driver !== 'none' ? (
            <Field>
              <FieldLabel htmlFor="periph-receipt-width">
                {t('desktop:peripherals.width')}
              </FieldLabel>
              <Select
                value={String(s.receipt.width ?? 80)}
                onValueChange={(v) =>
                  setS({ ...s, receipt: { ...s.receipt, width: v === '58' ? 58 : 80 } })
                }
              >
                <SelectTrigger id="periph-receipt-width" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="80">{t('desktop:peripherals.width80')}</SelectItem>
                  <SelectItem value="58">{t('desktop:peripherals.width58')}</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          ) : null}
        </FieldGroup>

        <FieldGroup>
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium">{t('desktop:peripherals.label')}</span>
            <div className="flex items-center gap-2">
              <ReadyBadge ready={bridge.data?.label.ready} />
              {testButton('label', s.label.driver !== 'none')}
            </div>
          </div>
          <Field>
            <FieldLabel htmlFor="periph-label-driver">{t('desktop:peripherals.driver')}</FieldLabel>
            <DriverSelect
              id="periph-label-driver"
              value={s.label.driver}
              options={['none', 'fake', 'zpl'] as const}
              labels="peripherals.drivers"
              onChange={(v) =>
                setS({
                  ...s,
                  label: { ...s.label, driver: v as PeripheralSettings['label']['driver'] },
                })
              }
            />
          </Field>
          {s.label.driver === 'zpl' ? (
            <Field>
              <FieldLabel htmlFor="periph-label-target">
                {t('desktop:peripherals.target')}
              </FieldLabel>
              <Input
                id="periph-label-target"
                className="font-mono"
                value={s.label.target ?? ''}
                onChange={(e) => setS({ ...s, label: { ...s.label, target: e.target.value } })}
              />
              <FieldDescription>{t('desktop:peripherals.targetHint')}</FieldDescription>
            </Field>
          ) : null}
        </FieldGroup>

        <FieldGroup>
          <span className="font-medium">{t('desktop:peripherals.reader')}</span>
          <Field>
            <FieldLabel htmlFor="periph-reader-driver">
              {t('desktop:peripherals.driver')}
            </FieldLabel>
            <DriverSelect
              id="periph-reader-driver"
              value={s.reader.driver}
              options={['keyboard', 'fake'] as const}
              labels="peripherals.drivers"
              onChange={(v) =>
                setS({ ...s, reader: { driver: v as PeripheralSettings['reader']['driver'] } })
              }
            />
            <FieldDescription>{t('desktop:peripherals.readerHint')}</FieldDescription>
          </Field>
        </FieldGroup>

        <FieldGroup>
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium">{t('desktop:peripherals.fiscal')}</span>
            <div className="flex items-center gap-2">
              <ReadyBadge ready={bridge.data?.fiscal.ready} />
              {testButton('fiscal', s.fiscal.driver !== 'none')}
            </div>
          </div>
          <Field>
            <FieldLabel htmlFor="periph-fiscal-driver">
              {t('desktop:peripherals.driver')}
            </FieldLabel>
            <DriverSelect
              id="periph-fiscal-driver"
              value={s.fiscal.driver}
              options={['none', 'fake'] as const}
              labels="peripherals.fiscalDrivers"
              onChange={(v) =>
                setS({ ...s, fiscal: { driver: v as PeripheralSettings['fiscal']['driver'] } })
              }
            />
            <FieldDescription>{t('desktop:peripherals.fiscalHint')}</FieldDescription>
          </Field>
          {s.fiscal.driver === 'fake' ? (
            <Field>
              <FieldLabel htmlFor="periph-fiscal-mode">
                {t('desktop:peripherals.fakeMode')}
              </FieldLabel>
              <DriverSelect
                id="periph-fiscal-mode"
                value={s.fiscal.fake_mode ?? 'ok'}
                options={['ok', 'offline', 'fail'] as const}
                labels="peripherals.fakeModes"
                onChange={(v) =>
                  setS({
                    ...s,
                    fiscal: {
                      ...s.fiscal,
                      fake_mode: v as NonNullable<PeripheralSettings['fiscal']['fake_mode']>,
                    },
                  })
                }
              />
            </Field>
          ) : null}
        </FieldGroup>
      </CardContent>
      <CardFooter>
        <Button type="button" disabled={!dirty || update.isPending} onClick={save}>
          {t('desktop:peripherals.save')}
        </Button>
      </CardFooter>
      <PinDialog {...gate.dialogProps} pending={update.isPending || test.isPending} />
    </>
  );
}

export function KioskCard({ status }: { status: DesktopStatus | null }) {
  const { t } = useT();
  const win = useAppWindowStatus();
  const set = useSetDesktopKiosk();
  const gate = usePinGate(win.remote);
  const [enabled, setEnabled] = useState<boolean | null>(null);
  return (
    <Card data-testid="desktop-kiosk-card">
      <CardHeader>
        <CardTitle>{t('desktop:kiosk.title')}</CardTitle>
        <CardDescription>{t('desktop:kiosk.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {set.error ? <ProblemAlert error={set.error} /> : null}
        <Field orientation="horizontal">
          <Switch
            id="kiosk-enabled"
            checked={enabled ?? status?.kiosk ?? false}
            disabled={set.isPending}
            onCheckedChange={(v) =>
              gate.run((pin) =>
                set.mutate(
                  { enabled: v, pin },
                  {
                    onSuccess: (r) => {
                      setEnabled(r.enabled);
                      toast.success(t('desktop:kiosk.saved'));
                    },
                  },
                ),
              )
            }
          />
          <FieldLabel htmlFor="kiosk-enabled" className="font-normal">
            {t('desktop:kiosk.enable')}
          </FieldLabel>
        </Field>
        {status?.kiosk ? (
          <p className="text-muted-foreground text-sm" data-testid="kiosk-active">
            {t('desktop:kiosk.active')} {t('desktop:kiosk.shortcut')}
          </p>
        ) : null}
      </CardContent>
      <PinDialog {...gate.dialogProps} pending={set.isPending} />
    </Card>
  );
}

/** The online instance of the remote mode: reachability, marina, version banner and unpairing (PIN). */
export function RemoteCard({ status }: { status: DesktopStatus }) {
  const { t, i18n } = useT();
  const unpair = useUnpairDesktopRemote();
  const [askPin, setAskPin] = useState(false);
  const r = status.remote;
  if (!r) return null;
  return (
    <Card data-testid="desktop-remote-card">
      <CardHeader>
        <CardTitle>{t('desktop:remote.title')}</CardTitle>
        <CardDescription>
          {t('desktop:remote.connectedTo', { marina: r.marina.name })}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {!r.reachable ? (
          <Alert data-testid="remote-unreachable">
            <CircleAlertIcon />
            <AlertTitle>{t('desktop:remote.unreachable')}</AlertTitle>
            <AlertDescription>{t('desktop:remote.unreachableHint')}</AlertDescription>
          </Alert>
        ) : null}
        {r.version_mismatch ? (
          <Alert>
            <AlertDescription>{t('desktop:remote.updateApp')}</AlertDescription>
          </Alert>
        ) : null}
        <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-3">
          <dt className="text-muted-foreground">{t('desktop:remote.address')}</dt>
          <dd className="font-mono break-all sm:col-span-2">{r.url}</dd>
          <dt className="text-muted-foreground">{t('desktop:remote.reachable')}</dt>
          <dd className="sm:col-span-2">
            <ReadyBadge ready={r.reachable} />
          </dd>
        </dl>
        <p className="text-muted-foreground text-sm">
          {t('desktop:remote.pairedAt', { date: formatDateTime(r.paired_at, i18n.language) })}
        </p>
        {unpair.isSuccess ? (
          <Alert data-testid="remote-unpaired">
            <AlertDescription>{t('desktop:restore.title')}</AlertDescription>
          </Alert>
        ) : null}
      </CardContent>
      <CardFooter>
        <ConfirmDialog
          trigger={
            <Button type="button" variant="destructive" disabled={unpair.isPending}>
              <UnlinkIcon />
              {t('desktop:remote.unpair')}
            </Button>
          }
          title={t('desktop:remote.unpairTitle')}
          description={t('desktop:remote.unpairDescription')}
          confirmLabel={t('desktop:remote.unpair')}
          cancelLabel={t('common:actions.cancel')}
          destructive
          onConfirm={() => setAskPin(true)}
        />
      </CardFooter>
      <PinDialog
        open={askPin}
        onOpenChange={setAskPin}
        pending={unpair.isPending}
        error={unpair.error}
        onConfirm={(pin) => unpair.mutate(pin, { onSuccess: () => setAskPin(false) })}
      />
    </Card>
  );
}

/** The CA download for phones on the local network (only for a name-constrained CA, m6b-plan §8.5). */
export function LanCaDownload({ status }: { status: LanStatus }) {
  const { t } = useT();
  if (!status.listening) return null;
  if (!status.ca_constrained) {
    return (
      <p className="text-muted-foreground text-sm" data-testid="lan-ca-regenerate">
        {t('desktop:lanCa.regenerate')}
      </p>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-3" data-testid="lan-ca-download">
      <Button variant="outline" size="sm" asChild>
        <a href={status.ca_download_url} download>
          {t('desktop:lanCa.download')}
        </a>
      </Button>
      <a
        href="/admin/help/certificate"
        className="text-primary text-sm font-medium underline underline-offset-4"
      >
        {t('desktop:lanCa.help')}
      </a>
    </div>
  );
}
