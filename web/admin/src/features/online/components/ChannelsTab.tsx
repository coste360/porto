import { PlugIcon, RefreshCwIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useBookingChannels,
  useChannelReservations,
  useSyncBookingChannel,
  useTestBookingChannel,
  useUpdateBookingChannel,
  type BookingChannel,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Skeleton } from '@porto/shared/ui/skeleton';
import { Switch } from '@porto/shared/ui/switch';
import { Textarea } from '@porto/shared/ui/textarea';

import { formatPairs, parsePairs } from '../lib/constants';

/** Booking channel connections (secrets write-only), category map, test, sync and recent reservations. */
export function ChannelsTab({ marinaId }: { marinaId: string }) {
  const { t } = useT();
  const channels = useBookingChannels(marinaId);
  if (channels.isPending) return <Skeleton className="h-48 w-full" />;
  if (channels.isError) return <ProblemAlert error={channels.error} />;
  return (
    <div className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">{t('online:settings.channels.description')}</p>
      {channels.data.items.length === 0 ? (
        <Card>
          <CardContent className="pt-6">
            <p className="text-muted-foreground text-sm">{t('online:settings.channels.empty')}</p>
          </CardContent>
        </Card>
      ) : (
        channels.data.items.map((c) => <ChannelCard key={c.code} marinaId={marinaId} channel={c} />)
      )}
      <Reservations marinaId={marinaId} />
    </div>
  );
}

function ChannelCard({ marinaId, channel: c }: { marinaId: string; channel: BookingChannel }) {
  const { t, i18n } = useT();
  const save = useUpdateBookingChannel(marinaId);
  const test = useTestBookingChannel(marinaId);
  const sync = useSyncBookingChannel(marinaId);
  const conn = c.connection;
  const [enabled, setEnabled] = useState(conn?.enabled ?? false);
  const [push, setPush] = useState(conn?.push_enabled ?? false);
  const [map, setMap] = useState(formatPairs(conn?.category_map ?? {}));
  const [credentials, setCredentials] = useState('');
  const [secret, setSecret] = useState('');
  const caps = Object.entries(c.capabilities)
    .filter(([, on]) => on)
    .map(([k]) => k)
    .join(', ');
  const id = (s: string) => `ch-${c.code}-${s}`;
  const submit = () =>
    save.mutate(
      {
        code: c.code,
        version: conn?.version ?? 0,
        body: {
          enabled,
          push_enabled: push,
          category_map: parsePairs(map),
          ...(credentials.trim() ? { credentials: parsePairs(credentials) } : {}),
          ...(secret ? { webhook_secret: secret } : {}),
        },
      },
      {
        onSuccess: () => {
          setCredentials('');
          setSecret('');
          toast.success(t('online:settings.channels.saved'));
        },
      },
    );
  return (
    <Card data-testid={`channel-${c.code}`}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <PlugIcon className="size-5" aria-hidden />
          {c.code}
          {conn?.enabled ? <Badge>{t('online:settings.channels.enabled')}</Badge> : null}
        </CardTitle>
        <CardDescription>
          {t('online:settings.channels.capabilities', { list: caps })}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <FieldGroup>
          <Field orientation="horizontal">
            <Switch id={id('enabled')} checked={enabled} onCheckedChange={setEnabled} />
            <FieldLabel htmlFor={id('enabled')}>{t('online:settings.channels.enabled')}</FieldLabel>
          </Field>
          <Field orientation="horizontal">
            <Switch
              id={id('push')}
              checked={push}
              onCheckedChange={setPush}
              disabled={!c.capabilities.push}
            />
            <FieldLabel htmlFor={id('push')}>{t('online:settings.channels.push')}</FieldLabel>
          </Field>
          <Field>
            <FieldLabel htmlFor={id('map')}>{t('online:settings.channels.categoryMap')}</FieldLabel>
            <Textarea
              id={id('map')}
              rows={3}
              value={map}
              onChange={(e) => setMap(e.target.value)}
              className="font-mono text-xs"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={id('cred')}>
              {t('online:settings.channels.credentials')}
            </FieldLabel>
            <Textarea
              id={id('cred')}
              rows={2}
              value={credentials}
              autoComplete="off"
              onChange={(e) => setCredentials(e.target.value)}
              className="font-mono text-xs"
            />
            {conn?.has_credentials ? (
              <FieldDescription>{t('online:settings.channels.credentialsSet')}</FieldDescription>
            ) : null}
          </Field>
          {c.capabilities.webhook ? (
            <>
              <Field>
                <FieldLabel htmlFor={id('secret')}>
                  {t('online:settings.channels.secret')}
                </FieldLabel>
                <Input
                  id={id('secret')}
                  type="password"
                  autoComplete="new-password"
                  value={secret}
                  onChange={(e) => setSecret(e.target.value)}
                />
                {conn?.has_webhook_secret ? (
                  <FieldDescription>{t('online:settings.channels.secretSet')}</FieldDescription>
                ) : null}
              </Field>
              {conn?.webhook_url ? (
                <Field>
                  <FieldLabel htmlFor={id('url')}>
                    {t('online:settings.channels.webhookUrl')}
                  </FieldLabel>
                  <Input
                    id={id('url')}
                    readOnly
                    value={conn.webhook_url}
                    className="font-mono text-xs"
                  />
                </Field>
              ) : null}
            </>
          ) : null}
        </FieldGroup>
        <div className="text-muted-foreground flex flex-col gap-1 text-sm">
          {conn?.last_pull_at ? (
            <span>
              {t('online:settings.channels.lastPull', {
                at: formatDateTime(conn.last_pull_at, i18n.language),
              })}
            </span>
          ) : null}
          {conn?.last_push_at ? (
            <span>
              {t('online:settings.channels.lastPush', {
                at: formatDateTime(conn.last_push_at, i18n.language),
              })}
            </span>
          ) : null}
          {conn?.last_error ? (
            <span>{t('online:settings.channels.lastError', { code: conn.last_error })}</span>
          ) : null}
        </div>
        <ProblemAlert error={save.error ?? test.error ?? sync.error} />
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button onClick={submit} disabled={save.isPending}>
            {t('online:settings.channels.save')}
          </Button>
          <Button
            variant="outline"
            disabled={!conn || test.isPending}
            onClick={() =>
              test.mutate(c.code, {
                onSuccess: (r) =>
                  r.ok
                    ? toast.success(t('online:settings.channels.testOk'))
                    : toast.error(
                        t('online:settings.channels.testFailed', { code: r.error_code ?? '' }),
                      ),
              })
            }
          >
            {t('online:settings.channels.test')}
          </Button>
          <Button
            variant="outline"
            disabled={!conn?.enabled || sync.isPending}
            onClick={() =>
              sync.mutate(c.code, {
                onSuccess: (r) => toast.success(t('online:settings.channels.synced', r)),
              })
            }
          >
            <RefreshCwIcon />
            {t('online:settings.channels.sync')}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function Reservations({ marinaId }: { marinaId: string }) {
  const { t, i18n } = useT();
  const q = useChannelReservations(marinaId);
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('online:settings.channels.reservations')}</CardTitle>
      </CardHeader>
      <CardContent>
        {q.isPending ? (
          <Skeleton className="h-16 w-full" />
        ) : q.isError ? (
          <ProblemAlert error={q.error} />
        ) : q.data.items.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            {t('online:settings.channels.noReservations')}
          </p>
        ) : (
          <ul className="flex flex-col gap-2 text-sm">
            {q.data.items.map((r) => (
              <li key={r.id} className="flex flex-wrap justify-between gap-2">
                <span>
                  {r.channel_code} · {r.external_ref} ·{' '}
                  {formatDateTime(r.created_at, i18n.language)}
                </span>
                <Badge variant={r.status === 'rejected' ? 'destructive' : 'outline'}>
                  {t(`online:settings.reservationStatus.${r.status}`)}
                </Badge>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
