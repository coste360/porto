import { useState } from 'react';

import {
  useCreateSensorGateway,
  useCreateSensorSource,
  usePollSensorSource,
  usePutSensorSettings,
  useRevokeSensorGatewayKey,
  useRotateSensorGatewayKey,
  useSensorGateways,
  useSensorSettings,
  useSensorSources,
  useUpdateSensorSource,
  type SensorGatewayKind,
  type SensorSettings,
  type SensorSettingsInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { ProblemAlert } from '@porto/shared/ui/patterns';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Switch } from '@porto/shared/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { EnumSelect } from '../../contracts-m4/components/kit';
import { GATEWAY_KINDS, settingsIssues } from '../lib';
import { KeyRevealDialog } from './KeyRevealDialog';

const CODE_RE = /^[A-Z0-9_-]{1,32}$/;

/** Gateways (m12-plan §3.2): code, name, kind, key status; create (key shown once), rotate, revoke. */
export function GatewaysTab({ marinaId }: { marinaId: string }) {
  const { t, i18n } = useT();
  const gateways = useSensorGateways(marinaId);
  const create = useCreateSensorGateway(marinaId);
  const rotate = useRotateSensorGatewayKey(marinaId);
  const revoke = useRevokeSensorGatewayKey(marinaId);
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const [kind, setKind] = useState<SensorGatewayKind>('lorawan');
  const [revealed, setRevealed] = useState<{ key: string; code: string } | null>(null);
  const fmt = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' });
  if (gateways.isPending) return <LoadingState rows={3} />;
  if (gateways.isError)
    return <QueryErrorState error={gateways.error} onRetry={() => void gateways.refetch()} />;
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>{t('sensors:gateways.new')}</CardTitle>
          <CardDescription>{t('sensors:gateways.hint')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap items-end gap-3">
          <Field className="w-40">
            <FieldLabel htmlFor="gw-code">{t('sensors:devices.code')}</FieldLabel>
            <Input
              id="gw-code"
              value={code}
              onChange={(e) => setCode(e.target.value.toUpperCase())}
            />
          </Field>
          <Field className="w-56">
            <FieldLabel htmlFor="gw-name">{t('sensors:gateways.name')}</FieldLabel>
            <Input
              id="gw-name"
              value={name}
              maxLength={80}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <EnumSelect
            id="gw-kind"
            label={t('sensors:devices.kind')}
            values={GATEWAY_KINDS}
            value={kind}
            onChange={(v) => v && setKind(v)}
            labelOf={(v) => t(`sensors:gatewayKind.${v}`)}
          />
          <Button
            disabled={!CODE_RE.test(code) || name.trim() === '' || create.isPending}
            onClick={() =>
              create.mutate(
                { code, name: name.trim(), kind },
                {
                  onSuccess: (r) => {
                    setRevealed({ key: r.key, code: r.gateway.code });
                    setCode('');
                    setName('');
                  },
                },
              )
            }
          >
            {t('sensors:gateways.create')}
          </Button>
        </CardContent>
      </Card>
      <ProblemAlert error={create.error ?? rotate.error ?? revoke.error} />
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('sensors:devices.code')}</TableHead>
            <TableHead>{t('sensors:gateways.name')}</TableHead>
            <TableHead>{t('sensors:devices.kind')}</TableHead>
            <TableHead className="text-right">{t('sensors:gateways.devices')}</TableHead>
            <TableHead>{t('sensors:devices.lastContact')}</TableHead>
            <TableHead>{t('sensors:devices.key')}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {gateways.data.items.map((g) => (
            <TableRow key={g.id}>
              <TableCell className="font-medium">{g.code}</TableCell>
              <TableCell>{g.name}</TableCell>
              <TableCell>{t(`sensors:gatewayKind.${g.kind}`)}</TableCell>
              <TableCell className="text-right tabular-nums">{g.device_count}</TableCell>
              <TableCell>
                {g.last_contact_at ? fmt.format(new Date(g.last_contact_at)) : '–'}
              </TableCell>
              <TableCell>{t(`sensors:keyStatus.${g.key_status}`)}</TableCell>
              <TableCell className="flex justify-end gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={rotate.isPending}
                  onClick={() =>
                    rotate.mutate(
                      { id: g.id, version: g.version, graceHours: 24 },
                      { onSuccess: (r) => setRevealed({ key: r.key, code: g.code }) },
                    )
                  }
                >
                  {t('sensors:devices.rotate')}
                </Button>
                {g.key_status !== 'revoked' ? (
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={revoke.isPending}
                    onClick={() => revoke.mutate({ id: g.id, version: g.version })}
                  >
                    {t('sensors:devices.revoke')}
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {revealed ? (
        <KeyRevealDialog
          secret={revealed.key}
          code={revealed.code}
          onClose={() => setRevealed(null)}
        />
      ) : null}
    </div>
  );
}

/** Polled sources (m12-plan §3.4): driver select, config fields from the driver keys, "Interroga ora". */
export function SourcesTab({ marinaId }: { marinaId: string }) {
  const { t, i18n } = useT();
  const sources = useSensorSources(marinaId);
  const create = useCreateSensorSource(marinaId);
  const update = useUpdateSensorSource(marinaId);
  const poll = usePollSensorSource(marinaId);
  const [driver, setDriver] = useState('');
  const [name, setName] = useState('');
  const [config, setConfig] = useState<Record<string, string>>({});
  const [polled, setPolled] = useState<{ id: string; accepted: number } | null>(null);
  const fmt = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' });
  if (sources.isPending) return <LoadingState rows={3} />;
  if (sources.isError)
    return <QueryErrorState error={sources.error} onRetry={() => void sources.refetch()} />;
  const drivers = sources.data.drivers;
  const d = drivers.find((x) => x.code === driver);
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>{t('sensors:sources.new')}</CardTitle>
          <CardDescription>{t('sensors:sources.hint')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {drivers.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('sensors:sources.noDrivers')}</p>
          ) : (
            <div className="flex flex-wrap items-end gap-3">
              <EnumSelect
                id="src-driver"
                label={t('sensors:sources.driver')}
                values={drivers.map((x) => x.code)}
                value={driver}
                onChange={(v) => {
                  setDriver(v);
                  setConfig({});
                }}
                labelOf={(c) => c}
              />
              <Field className="w-56">
                <FieldLabel htmlFor="src-name">{t('sensors:gateways.name')}</FieldLabel>
                <Input
                  id="src-name"
                  value={name}
                  maxLength={80}
                  onChange={(e) => setName(e.target.value)}
                />
              </Field>
              {d
                ? [...d.public_keys, ...d.secret_keys].map((k) => (
                    <Field key={k} className="w-48">
                      <FieldLabel htmlFor={`src-cfg-${k}`}>{k}</FieldLabel>
                      <Input
                        id={`src-cfg-${k}`}
                        type={d.secret_keys.includes(k) ? 'password' : 'text'}
                        autoComplete="off"
                        value={config[k] ?? ''}
                        onChange={(e) => setConfig({ ...config, [k]: e.target.value })}
                      />
                    </Field>
                  ))
                : null}
              <Button
                disabled={!d || name.trim() === '' || create.isPending}
                onClick={() =>
                  create.mutate(
                    { code: driver, name: name.trim(), config, poll_seconds: 60, enabled: true },
                    {
                      onSuccess: () => {
                        setName('');
                        setConfig({});
                      },
                    },
                  )
                }
              >
                {t('sensors:sources.create')}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
      <ProblemAlert error={create.error ?? update.error ?? poll.error} />
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('sensors:gateways.name')}</TableHead>
            <TableHead>{t('sensors:sources.driver')}</TableHead>
            <TableHead className="text-right">{t('sensors:gateways.devices')}</TableHead>
            <TableHead>{t('sensors:sources.lastPolled')}</TableHead>
            <TableHead>{t('sensors:sources.state')}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {sources.data.items.map((s) => (
            <TableRow key={s.id}>
              <TableCell className="font-medium">{s.name}</TableCell>
              <TableCell>{s.code}</TableCell>
              <TableCell className="text-right tabular-nums">{s.device_count}</TableCell>
              <TableCell>
                {s.last_polled_at ? fmt.format(new Date(s.last_polled_at)) : '–'}
              </TableCell>
              <TableCell>
                {s.last_error_code ? (
                  <Badge variant="destructive">
                    {t(`sensors:sourceError.${s.last_error_code}`)}
                  </Badge>
                ) : s.enabled ? (
                  <Badge variant="secondary">{t('sensors:sources.enabled')}</Badge>
                ) : (
                  <Badge variant="outline">{t('sensors:sources.disabled')}</Badge>
                )}
                {polled?.id === s.id ? (
                  <span className="text-muted-foreground ml-2 text-xs">
                    {t('sensors:sources.polled', { count: polled.accepted })}
                  </span>
                ) : null}
              </TableCell>
              <TableCell className="flex justify-end gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={poll.isPending}
                  onClick={() =>
                    poll.mutate(s.id, {
                      onSuccess: (r) => setPolled({ id: s.id, accepted: r.accepted }),
                    })
                  }
                >
                  {t('sensors:sources.pollNow')}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={update.isPending}
                  onClick={() =>
                    update.mutate({ id: s.id, version: s.version, body: { enabled: !s.enabled } })
                  }
                >
                  {s.enabled ? t('sensors:sources.disable') : t('sensors:sources.enable')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

const NUMERIC_FIELDS = [
  'hysteresis_readings',
  'hysteresis_minutes',
  'offline_after_minutes',
  'silent_alert_minutes',
  'battery_low_pct',
  'unexpected_after_minutes',
  'departure_cutoff_minutes',
  'presence_out_after_hours',
  'release_proposal_after_days',
  'release_proposal_nights',
] as const;

/** Settings (m12-plan §6): thresholds with range checks and the presence-effect switch explained. */
export function SettingsTab({
  marinaId,
  canConfigure,
}: {
  marinaId: string;
  canConfigure: boolean;
}) {
  const settings = useSensorSettings(marinaId);
  if (settings.isPending) return <LoadingState rows={5} />;
  if (settings.isError)
    return <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />;
  return (
    <SettingsForm
      key={settings.data.version}
      marinaId={marinaId}
      settings={settings.data}
      canConfigure={canConfigure}
    />
  );
}

export function SettingsForm({
  marinaId,
  settings,
  canConfigure,
}: {
  marinaId: string;
  settings: SensorSettings;
  canConfigure: boolean;
}) {
  const { t } = useT();
  const put = usePutSensorSettings(marinaId);
  const [form, setForm] = useState<Record<(typeof NUMERIC_FIELDS)[number], string>>(
    () =>
      Object.fromEntries(NUMERIC_FIELDS.map((k) => [k, String(settings[k])])) as Record<
        (typeof NUMERIC_FIELDS)[number],
        string
      >,
  );
  const [effect, setEffect] = useState(settings.presence_effect_enabled);
  const body: SensorSettingsInput = {
    ...(Object.fromEntries(NUMERIC_FIELDS.map((k) => [k, Number(form[k])])) as Omit<
      SensorSettingsInput,
      'presence_effect_enabled'
    >),
    presence_effect_enabled: effect,
  };
  const issues = settingsIssues(body);
  return (
    <fieldset
      disabled={!canConfigure}
      className="flex flex-col gap-4"
      data-testid="sensor-settings"
    >
      <Card>
        <CardHeader>
          <CardTitle>{t('sensors:settings.thresholds')}</CardTitle>
          <CardDescription>
            {t('sensors:settings.retention', { days: settings.raw_retention_days })}
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {NUMERIC_FIELDS.map((k) => (
            <Field key={k} data-invalid={issues.includes(k) || undefined}>
              <FieldLabel htmlFor={`set-${k}`}>{t(`sensors:settings.fields.${k}`)}</FieldLabel>
              <Input
                id={`set-${k}`}
                inputMode="numeric"
                value={form[k]}
                aria-invalid={issues.includes(k)}
                onChange={(e) => setForm({ ...form, [k]: e.target.value })}
              />
              {issues.includes(k) ? (
                <FieldDescription className="text-destructive">
                  {t('sensors:settings.outOfRange')}
                </FieldDescription>
              ) : null}
            </Field>
          ))}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('sensors:settings.effectTitle')}</CardTitle>
          <CardDescription>{t('sensors:settings.effectHint')}</CardDescription>
        </CardHeader>
        <CardContent>
          <Field orientation="horizontal">
            <Switch id="set-effect" checked={effect} onCheckedChange={setEffect} />
            <FieldLabel htmlFor="set-effect">{t('sensors:settings.effect')}</FieldLabel>
          </Field>
        </CardContent>
      </Card>
      <ProblemAlert error={put.error} />
      {canConfigure ? (
        <div>
          <Button
            disabled={issues.length > 0 || put.isPending}
            data-testid="settings-save"
            onClick={() => put.mutate({ version: settings.version, body })}
          >
            {t('common:actions.save')}
          </Button>
        </div>
      ) : null}
    </fieldset>
  );
}
