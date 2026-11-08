import type { ColumnDef } from '@tanstack/react-table';
import {
  CopyIcon,
  DatabaseBackupIcon,
  FolderOpenIcon,
  KeyRoundIcon,
  RefreshCwIcon,
  TriangleAlertIcon,
  UploadIcon,
} from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { toast } from 'sonner';

import {
  useCreateDesktopBackup,
  useDesktopBackups,
  useDesktopLan,
  useDesktopSettings,
  useOpenDesktopFolder,
  useRestoreDesktopBackup,
  useRevealDesktopSecretKey,
  useRotateLanCertificate,
  useSetDesktopLan,
  useUpdateDesktopSettings,
  useUploadDesktopRestore,
  type DesktopBackupFile,
  type DesktopSettings,
  type DesktopStatus,
  type LanStatus,
} from '@porto/shared/api';
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@porto/shared/ui/dialog';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ConfirmDialog, DataTable, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';

import { ForbiddenState, LoadingState, QueryErrorState } from '../../components/PageStates';
import { toastProblem } from '../../lib/toast';
import { formatBytes, useDesktopAccess } from './lib';
import { KioskCard, LanCaDownload, PeripheralsCard, RemoteCard } from './M6bCards';
import { RestartingNotice } from './RestartingNotice';

const AUTO = 'auto';

/**
 * Desktop app settings (owner, loopback only, m6a-plan §4.4): local network, backups, updates, secret key, data.
 * Elsewhere (browser on another computer, server edition) the page explains it is only in the app window.
 */
export function DesktopSettingsPage() {
  const { t } = useT();
  const access = useDesktopAccess();
  const [restarting, setRestarting] = useState(false);

  if (access.resolving) return <LoadingState rows={6} />;
  if (!access.loopback) {
    return (
      <div data-testid="desktop-settings-page">
        <PageHeader title={t('desktop:settings.title')} />
        <Alert>
          <AlertDescription>{t('desktop:settings.onlyInApp')}</AlertDescription>
        </Alert>
      </div>
    );
  }
  if (access.remote && access.status) {
    // M6b remote mode: no local users, no database: the online instance, peripherals and kiosk (app PIN).
    return (
      <div data-testid="desktop-settings-page" className="flex flex-col gap-6">
        <PageHeader
          title={t('desktop:settings.title')}
          description={t('desktop:peripherals.description')}
        />
        <RemoteCard status={access.status} />
        <PeripheralsCard />
        <KioskCard status={access.status} />
      </div>
    );
  }
  if (!access.owner) return <ForbiddenState />;
  if (restarting) return <RestartingNotice />;
  return (
    <div data-testid="desktop-settings-page" className="flex flex-col gap-6">
      <PageHeader
        title={t('desktop:settings.title')}
        description={t('desktop:settings.description')}
      />
      <SettingsCards status={access.status} onRestart={() => setRestarting(true)} />
    </div>
  );
}

function SettingsCards({
  status,
  onRestart,
}: {
  status: DesktopStatus | null;
  onRestart: () => void;
}) {
  const settings = useDesktopSettings();
  if (settings.isPending) return <LoadingState rows={6} />;
  if (settings.isError)
    return <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />;
  return (
    <>
      <LanCard />
      <PeripheralsCard />
      <KioskCard status={status} />
      <BackupCard settings={settings.data} onRestart={onRestart} />
      <UpdatesCard settings={settings.data} status={status} />
      <SecretKeyCard status={status} />
      <DataCard />
    </>
  );
}

// ---- local network ----

function LanCard() {
  const lan = useDesktopLan();
  if (lan.isPending) return <LoadingState rows={3} />;
  if (lan.isError) return <QueryErrorState error={lan.error} onRetry={() => void lan.refetch()} />;
  // Remount the form when the server-side address or port changes, so the inputs show the applied values.
  return <LanForm key={`${lan.data.address}:${lan.data.port}`} status={lan.data} />;
}

function LanForm({ status }: { status: LanStatus }) {
  const { t } = useT();
  const set = useSetDesktopLan();
  const rotate = useRotateLanCertificate();
  const [address, setAddress] = useState(status.address || AUTO);
  const [port, setPort] = useState(String(status.port));
  const portNumber = Number.parseInt(port, 10);
  const portValid = Number.isInteger(portNumber) && portNumber >= 1024 && portNumber <= 65535;

  const apply = (enabled: boolean) =>
    set.mutate({
      enabled,
      ...(portValid ? { port: portNumber } : {}),
      address: address === AUTO ? '' : address,
    });

  return (
    <Card data-testid="desktop-lan-card">
      <CardHeader>
        <CardTitle>{t('desktop:lan.title')}</CardTitle>
        <CardDescription>{t('desktop:lan.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <Switch
            id="lan-enabled"
            checked={status.enabled}
            disabled={set.isPending}
            onCheckedChange={(checked) => apply(checked)}
          />
          <FieldLabel htmlFor="lan-enabled">{t('desktop:lan.enable')}</FieldLabel>
          {status.enabled ? (
            <Badge variant={status.listening ? 'secondary' : 'destructive'}>
              {status.listening ? t('desktop:lan.listening') : t('desktop:lan.notListening')}
            </Badge>
          ) : null}
        </div>
        {set.error ? <ProblemAlert error={set.error} /> : null}
        {!set.error && status.error ? (
          <Alert variant="destructive" data-testid="lan-error">
            <AlertDescription>{t(`errors:${status.error}`)}</AlertDescription>
          </Alert>
        ) : null}
        {status.enabled && status.listening ? (
          <LanDetails status={status} />
        ) : (
          <p className="text-muted-foreground text-sm">{t('desktop:lan.off')}</p>
        )}
        <FieldGroup className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="lan-address">{t('desktop:lan.address')}</FieldLabel>
            <Select value={address} onValueChange={setAddress}>
              <SelectTrigger id="lan-address" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={AUTO}>{t('desktop:lan.addressAuto')}</SelectItem>
                {status.addresses.map((a) => (
                  <SelectItem key={a} value={a}>
                    {a}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field data-invalid={portValid ? undefined : true}>
            <FieldLabel htmlFor="lan-port">{t('desktop:lan.port')}</FieldLabel>
            <Input
              id="lan-port"
              inputMode="numeric"
              value={port}
              aria-invalid={!portValid}
              onChange={(e) => setPort(e.target.value.replace(/\D/g, '').slice(0, 5))}
            />
            {portValid ? null : <FieldDescription>{t('desktop:lan.portInvalid')}</FieldDescription>}
          </Field>
        </FieldGroup>
        <Alert>
          <TriangleAlertIcon />
          <AlertDescription>{t('desktop:settings.openWarning')}</AlertDescription>
        </Alert>
        <LanCaDownload status={status} />
      </CardContent>
      <CardFooter className="flex flex-wrap gap-2">
        {status.enabled ? (
          <Button
            type="button"
            variant="outline"
            disabled={set.isPending || !portValid}
            onClick={() => apply(true)}
          >
            {t('desktop:lan.apply')}
          </Button>
        ) : null}
        {status.enabled && status.listening ? (
          <>
            <ConfirmDialog
              trigger={
                <Button type="button" variant="outline" disabled={rotate.isPending}>
                  <RefreshCwIcon />
                  {t('desktop:lan.rotate')}
                </Button>
              }
              title={t('desktop:lan.rotate')}
              description={t('desktop:lan.rotateConfirm')}
              confirmLabel={t('desktop:lan.rotate')}
              cancelLabel={t('common:actions.cancel')}
              onConfirm={() => rotate.mutate(false, { onError: (e) => toastProblem(t, e) })}
            />
            <ConfirmDialog
              trigger={
                <Button type="button" variant="ghost" disabled={rotate.isPending}>
                  {t('desktop:lan.rotateAll')}
                </Button>
              }
              title={t('desktop:lan.rotateAll')}
              description={t('desktop:lan.rotateAllConfirm')}
              confirmLabel={t('desktop:lan.rotateAll')}
              cancelLabel={t('common:actions.cancel')}
              destructive
              onConfirm={() => rotate.mutate(true, { onError: (e) => toastProblem(t, e) })}
            />
          </>
        ) : null}
      </CardFooter>
    </Card>
  );
}

function LanDetails({ status }: { status: LanStatus }) {
  const { t, i18n } = useT();
  return (
    <dl className="grid gap-3 text-sm sm:grid-cols-2" data-testid="lan-details">
      <div className="flex flex-col gap-1">
        <dt className="text-muted-foreground">{t('desktop:lan.url')}</dt>
        <dd className="flex items-center gap-2">
          <code className="font-mono break-all" data-testid="lan-url">
            {status.url}
          </code>
          <CopyValue value={status.url} />
        </dd>
      </div>
      <div className="flex flex-col gap-1">
        <dt className="text-muted-foreground">{t('desktop:lan.fingerprint')}</dt>
        <dd>
          <code className="font-mono text-xs break-all">{status.fingerprint}</code>
        </dd>
      </div>
      {status.cert_expires_at ? (
        <div className="flex flex-col gap-1 sm:col-span-2">
          <dd className="text-muted-foreground">
            {t('desktop:lan.certExpires', {
              date: formatDateTime(status.cert_expires_at, i18n.language),
            })}
          </dd>
        </div>
      ) : null}
    </dl>
  );
}

function CopyValue({ value }: { value: string }) {
  const { t } = useT();
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={t('common:actions.copy')}
      title={t('common:actions.copy')}
      onClick={() =>
        void navigator.clipboard
          ?.writeText(value)
          .then(() => toast.success(t('common:actions.copied')))
          .catch(() => undefined)
      }
    >
      <CopyIcon />
    </Button>
  );
}

// ---- backups ----

function BackupCard({ settings, onRestart }: { settings: DesktopSettings; onRestart: () => void }) {
  const { t, i18n } = useT();
  const backups = useDesktopBackups();
  const create = useCreateDesktopBackup();
  const restore = useRestoreDesktopBackup();
  const update = useUpdateDesktopSettings();
  const open = useOpenDesktopFolder();
  const [dir, setDir] = useState(settings.backup_dir);
  const [keepDaily, setKeepDaily] = useState(String(settings.backup_keep_daily));
  const [keepExit, setKeepExit] = useState(String(settings.backup_keep_exit));

  const daily = Number.parseInt(keepDaily, 10);
  const exit = Number.parseInt(keepExit, 10);
  const dailyValid = Number.isInteger(daily) && daily >= 1 && daily <= 365;
  const exitValid = Number.isInteger(exit) && exit >= 1 && exit <= 50;
  const dirty =
    dir.trim() !== settings.backup_dir ||
    daily !== settings.backup_keep_daily ||
    exit !== settings.backup_keep_exit;

  const save = (event: FormEvent) => {
    event.preventDefault();
    if (!dailyValid || !exitValid) return;
    update.mutate(
      { backup_dir: dir.trim(), backup_keep_daily: daily, backup_keep_exit: exit },
      { onSuccess: () => toast.success(t('common:status.saved')) },
    );
  };

  const columns: ColumnDef<DesktopBackupFile, unknown>[] = [
    {
      header: t('desktop:backup.date'),
      cell: ({ row }) => formatDateTime(row.original.created_at, i18n.language),
    },
    {
      header: t('desktop:backup.kind'),
      cell: ({ row }) => t(`desktop:backup.kinds.${row.original.kind}`),
    },
    {
      header: t('desktop:backup.size'),
      cell: ({ row }) => formatBytes(row.original.size_bytes, i18n.language),
    },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <ConfirmDialog
            trigger={
              <Button variant="ghost" size="sm" disabled={restore.isPending}>
                {t('desktop:backup.restore')}
              </Button>
            }
            title={t('desktop:backup.restoreTitle')}
            description={t('desktop:backup.restoreConfirm')}
            confirmLabel={t('desktop:backup.restore')}
            cancelLabel={t('common:actions.cancel')}
            destructive
            onConfirm={() =>
              restore.mutate(row.original.id, {
                onSuccess: onRestart,
                onError: (e) => toastProblem(t, e),
              })
            }
          />
        </div>
      ),
    },
  ];

  const status = backups.data?.status;
  return (
    <Card data-testid="desktop-backup-card">
      <CardHeader>
        <CardTitle>{t('desktop:backup.title')}</CardTitle>
        <CardDescription>{t('desktop:backup.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {status?.fallback ? (
          <Alert data-testid="backup-fallback">
            <TriangleAlertIcon />
            <AlertDescription>{t('desktop:backup.fallback')}</AlertDescription>
          </Alert>
        ) : null}
        {status?.last_error ? (
          <Alert variant="destructive">
            <AlertTitle>{t('desktop:backup.lastError')}</AlertTitle>
            <AlertDescription>{status.last_error}</AlertDescription>
          </Alert>
        ) : null}
        {status ? (
          <p className="text-muted-foreground text-sm">
            {status.last_daily_at
              ? t('desktop:backup.lastDaily', {
                  date: formatDateTime(status.last_daily_at, i18n.language),
                })
              : t('desktop:backup.noDaily')}
            {status.next_due_at
              ? ` ${t('desktop:backup.nextDue', {
                  date: formatDateTime(status.next_due_at, i18n.language),
                })}`
              : null}
          </p>
        ) : null}
        <form className="flex flex-col gap-4" onSubmit={save} noValidate>
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <Field className="sm:col-span-2">
              <FieldLabel htmlFor="backup-dir">{t('desktop:backup.folder')}</FieldLabel>
              <div className="flex gap-2">
                <Input
                  id="backup-dir"
                  value={dir}
                  placeholder={status?.dir ?? ''}
                  onChange={(e) => setDir(e.target.value)}
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => open.mutate('backup_dir', { onError: (e) => toastProblem(t, e) })}
                >
                  <FolderOpenIcon />
                  {t('desktop:backup.openFolder')}
                </Button>
              </div>
              <FieldDescription>{t('desktop:backup.folderHint')}</FieldDescription>
            </Field>
            <Field data-invalid={dailyValid ? undefined : true}>
              <FieldLabel htmlFor="backup-keep-daily">{t('desktop:backup.keepDaily')}</FieldLabel>
              <Input
                id="backup-keep-daily"
                inputMode="numeric"
                value={keepDaily}
                aria-invalid={!dailyValid}
                onChange={(e) => setKeepDaily(e.target.value.replace(/\D/g, '').slice(0, 3))}
              />
            </Field>
            <Field data-invalid={exitValid ? undefined : true}>
              <FieldLabel htmlFor="backup-keep-exit">{t('desktop:backup.keepExit')}</FieldLabel>
              <Input
                id="backup-keep-exit"
                inputMode="numeric"
                value={keepExit}
                aria-invalid={!exitValid}
                onChange={(e) => setKeepExit(e.target.value.replace(/\D/g, '').slice(0, 2))}
              />
            </Field>
          </FieldGroup>
          {update.error ? <ProblemAlert error={update.error} /> : null}
          <div>
            <Button
              type="submit"
              variant="outline"
              disabled={!dirty || !dailyValid || !exitValid || update.isPending}
            >
              {t('common:actions.save')}
            </Button>
          </div>
        </form>
        {backups.isPending ? (
          <LoadingState rows={3} />
        ) : backups.isError ? (
          <QueryErrorState error={backups.error} onRetry={() => void backups.refetch()} />
        ) : (
          <DataTable
            columns={columns}
            data={backups.data.items}
            getRowId={(b) => b.id}
            empty={t('desktop:backup.empty')}
          />
        )}
      </CardContent>
      <CardFooter className="flex flex-wrap gap-2">
        <Button
          type="button"
          disabled={create.isPending}
          onClick={() =>
            create.mutate(undefined, {
              onSuccess: () => toast.success(t('desktop:backup.done')),
              onError: (e) => toastProblem(t, e),
            })
          }
        >
          <DatabaseBackupIcon />
          {t('desktop:backup.now')}
        </Button>
        <UploadRestore onRestart={onRestart} />
      </CardFooter>
    </Card>
  );
}

function UploadRestore({ onRestart }: { onRestart: () => void }) {
  const { t } = useT();
  const upload = useUploadDesktopRestore();
  const [file, setFile] = useState<File | null>(null);
  return (
    <Dialog
      onOpenChange={(open) => {
        if (!open) {
          setFile(null);
          upload.reset();
        }
      }}
    >
      <DialogTrigger asChild>
        <Button type="button" variant="outline">
          <UploadIcon />
          {t('desktop:backup.restoreFile')}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desktop:backup.restoreFile')}</DialogTitle>
          <DialogDescription>{t('desktop:backup.restoreConfirm')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="restore-file">{t('desktop:backup.file')}</FieldLabel>
          <Input
            id="restore-file"
            type="file"
            accept=".gz,.tgz,application/gzip"
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
          />
        </Field>
        {upload.error ? <ProblemAlert error={upload.error} /> : null}
        <DialogFooter>
          <Button
            type="button"
            variant="destructive"
            disabled={!file || upload.isPending}
            onClick={() => file && upload.mutate(file, { onSuccess: onRestart })}
          >
            {t('desktop:backup.restore')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---- updates ----

function UpdatesCard({
  settings,
  status,
}: {
  settings: DesktopSettings;
  status: DesktopStatus | null;
}) {
  const { t } = useT();
  const update = useUpdateDesktopSettings();
  return (
    <Card data-testid="desktop-updates-card">
      <CardHeader>
        <CardTitle>{t('desktop:updates.title')}</CardTitle>
        <CardDescription>{t('desktop:updates.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <Switch
            id="update-check"
            checked={settings.update_check}
            disabled={update.isPending}
            onCheckedChange={(checked) =>
              update.mutate({ update_check: checked }, { onError: (e) => toastProblem(t, e) })
            }
          />
          <FieldLabel htmlFor="update-check">{t('desktop:updates.check')}</FieldLabel>
        </div>
        <p className="text-muted-foreground text-sm">
          {t('desktop:updates.current', { version: status?.version ?? '' })}
          {status?.update
            ? ` ${t('desktop:updates.available', { version: status.update.version })}`
            : null}
        </p>
      </CardContent>
    </Card>
  );
}

// ---- secret key ----

function SecretKeyCard({ status }: { status: DesktopStatus | null }) {
  const { t } = useT();
  return (
    <Card data-testid="desktop-secret-card">
      <CardHeader>
        <CardTitle>{t('desktop:secret.title')}</CardTitle>
        <CardDescription>{t('desktop:secret.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {status?.secret_store === 'file' ? (
          <Alert data-testid="secret-file-fallback">
            <TriangleAlertIcon />
            <AlertDescription>{t('desktop:secret.fileFallback')}</AlertDescription>
          </Alert>
        ) : null}
        <div>
          <RevealSecretDialog />
        </div>
      </CardContent>
    </Card>
  );
}

/** Password re-entry, then the key once (kept in this component's state only, cleared on close). */
export function RevealSecretDialog() {
  const { t } = useT();
  const reveal = useRevealDesktopSecretKey();
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState('');
  const [secret, setSecret] = useState<string | null>(null);

  const onOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setPassword('');
      setSecret(null);
      reveal.reset();
    }
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!password) return;
    reveal.mutate(password, {
      onSuccess: (data) => {
        setSecret(data.secret_key);
        setPassword('');
      },
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>
        <Button type="button" variant="outline">
          <KeyRoundIcon />
          {t('desktop:secret.reveal')}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desktop:secret.title')}</DialogTitle>
          <DialogDescription>{t('desktop:secret.warning')}</DialogDescription>
        </DialogHeader>
        {secret ? (
          <div className="flex flex-col gap-2" data-testid="secret-key">
            <FieldLabel htmlFor="secret-key-value">{t('desktop:secret.title')}</FieldLabel>
            <div className="flex gap-2">
              <Input id="secret-key-value" readOnly value={secret} className="font-mono" />
              <CopyValue value={secret} />
            </div>
          </div>
        ) : (
          <form className="flex flex-col gap-4" onSubmit={submit} noValidate>
            <Field>
              <FieldLabel htmlFor="secret-password">{t('desktop:secret.password')}</FieldLabel>
              <Input
                id="secret-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
            {reveal.error ? <ProblemAlert error={reveal.error} /> : null}
            <DialogFooter>
              <Button type="submit" disabled={!password || reveal.isPending}>
                {t('desktop:secret.reveal')}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ---- data ----

function DataCard() {
  const { t } = useT();
  const open = useOpenDesktopFolder();
  const onError = (e: unknown) => toastProblem(t, e);
  return (
    <Card data-testid="desktop-data-card">
      <CardHeader>
        <CardTitle>{t('desktop:data.title')}</CardTitle>
        <CardDescription>{t('desktop:data.description')}</CardDescription>
      </CardHeader>
      <CardFooter className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => open.mutate('data_dir', { onError })}
        >
          <FolderOpenIcon />
          {t('desktop:data.openData')}
        </Button>
        <Button type="button" variant="outline" onClick={() => open.mutate('logs', { onError })}>
          <FolderOpenIcon />
          {t('desktop:data.openLogs')}
        </Button>
      </CardFooter>
    </Card>
  );
}
