// Query and mutation hooks of the desktop edition (m6a-plan §3.4, §4): instance meta, the loopback-only
// `/api/desktop/v1` surface (status, first run, settings, local network, backups, restore, secret key, open
// folder) and the browser pairing of phones (`pairDeviceSession`). Desktop keys live under ['desktop'], are not
// marina-scoped and are never invalidated by SSE. Secrets (first-run token, revealed key, pairing code) are only
// request/response values: nothing here caches them.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { unwrap } from '../client';
import { useApi } from '../context';
import { ProblemError } from '../problem';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

const D = '/api/desktop/v1' as const;

export type InstanceMeta = S['Meta'];
export type DesktopStatus = S['DesktopStatus'];
export type DesktopSettings = S['DesktopSettings'];
export type DesktopSettingsPatch = S['DesktopSettingsPatch'];
export type DesktopFirstRunRequest = S['DesktopFirstRunRequest'];
export type LanStatus = S['LanStatus'];
export type LanUpdate = S['LanUpdate'];
export type DesktopBackupFile = S['DesktopBackupFile'];
export type DesktopBackupKind = S['DesktopBackupKind'];
export type DesktopBackupList = S['DesktopBackupList'];
export type DesktopOpenTarget = S['DesktopOpenRequest']['target'];
export type PairSessionRequest = S['PairSessionRequest'];
export type PairSessionResult = S['PairSessionResult'];
export type DeviceSessionTotpRequest = S['DeviceSessionTotpRequest'];
export type DeviceAccess = S['DeviceAccess'];

/** Instance metadata (public): edition, local mode, capabilities. Changes only on restart. */
export function useInstanceMeta() {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.meta,
    queryFn: () => unwrap(api.GET('/api/admin/v1/meta')),
    staleTime: 5 * 60_000,
  });
}

/** True when the instance runs in local mode (desktop edition: no public page, online payments, remote signature). */
export function useLocalMode(): boolean {
  return useInstanceMeta().data?.local_mode ?? false;
}

/**
 * Status of the desktop app. Fetched only on the desktop edition; it answers only in the app window
 * (loopback), so a 404 (local-network client) means "not here" and resolves to null.
 */
export function useDesktopStatus() {
  const api = useApi();
  const meta = useInstanceMeta();
  const desktop = meta.data?.edition === 'desktop';
  const query = useQuery({
    queryKey: queryKeys.desktop.status,
    enabled: desktop,
    retry: false,
    staleTime: 60_000,
    queryFn: async (): Promise<DesktopStatus | null> => {
      try {
        return await unwrap(api.GET(`${D}/status`));
      } catch (error) {
        if (error instanceof ProblemError && error.status === 404) return null;
        throw error;
      }
    },
  });
  return {
    ...query,
    /** True in the app window of the desktop edition (the status answered). */
    loopback: desktop && !!query.data,
    /** Still finding out (meta, then status on the desktop edition). */
    resolving: meta.isPending || (desktop && query.isPending),
  };
}

/** First run without a session: creates the owner and the organization; the response sets the cookies. */
export function useDesktopFirstRun() {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: DesktopFirstRunRequest) => unwrap(api.POST(`${D}/first-run`, { body })),
    onSuccess: async () => {
      // A new principal: the next guard check fetches `me` again.
      qc.removeQueries({ queryKey: queryKeys.me });
      await qc.invalidateQueries({ queryKey: queryKeys.desktop.all });
    },
  });
}

/** First run: restore an uploaded backup instead of creating the owner (token in a header, never the URL). */
export function useDesktopFirstRunRestore() {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { token: string; file: Blob }) =>
      unwrap(
        api.POST(`${D}/first-run/restore`, {
          params: { header: { 'Porto-First-Run-Token': v.token } },
          body: v.file as never,
          bodySerializer: (b: unknown) => b as Blob,
          headers: { 'Content-Type': 'application/gzip' },
        }),
      ),
  });
}

export function useDesktopSettings(enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.desktop.settings,
    enabled,
    queryFn: () => unwrap(api.GET(`${D}/settings`)),
  });
}

export function useUpdateDesktopSettings() {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: DesktopSettingsPatch) => unwrap(api.PATCH(`${D}/settings`, { body })),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.desktop.settings, data);
      // The update banner depends on update_check and dismissed_update.
      void qc.invalidateQueries({ queryKey: queryKeys.desktop.status });
      void qc.invalidateQueries({ queryKey: queryKeys.desktop.backups });
    },
  });
}

export function useDesktopLan(enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.desktop.lan,
    enabled,
    queryFn: () => unwrap(api.GET(`${D}/lan`)),
  });
}

function useLanMutation<V>(fn: (v: V) => Promise<LanStatus>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.desktop.lan, data);
      void qc.invalidateQueries({ queryKey: queryKeys.desktop.settings });
      // The desktop.lan capability changes with the listener.
      void qc.invalidateQueries({ queryKey: queryKeys.meta });
    },
    onError: () => void qc.invalidateQueries({ queryKey: queryKeys.desktop.lan }),
  });
}

export function useSetDesktopLan() {
  const api = useApi();
  return useLanMutation((body: LanUpdate) => unwrap(api.PUT(`${D}/lan`, { body })));
}

export function useRotateLanCertificate() {
  const api = useApi();
  return useLanMutation((all: boolean) =>
    unwrap(api.POST(`${D}/lan/certificate`, { body: { all } })),
  );
}

export function useDesktopBackups(enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.desktop.backups,
    enabled,
    queryFn: () => unwrap(api.GET(`${D}/backups`)),
  });
}

export function useCreateDesktopBackup() {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(api.POST(`${D}/backups`)),
    onSettled: () => void qc.invalidateQueries({ queryKey: queryKeys.desktop.backups }),
  });
}

/** Restores a listed backup; 202 means the server restarts on it (everybody logs in again). */
export function useRestoreDesktopBackup() {
  const api = useApi();
  return useMutation({
    mutationFn: (backupId: string) =>
      unwrap(api.POST(`${D}/backups/{backupId}/restore`, { params: { path: { backupId } } })),
  });
}

/** Restores an uploaded `.tar.gz` (raw body, streamed by the server). */
export function useUploadDesktopRestore() {
  const api = useApi();
  return useMutation({
    mutationFn: (file: Blob) =>
      unwrap(
        api.POST(`${D}/restore-upload`, {
          body: file as never,
          bodySerializer: (b: unknown) => b as Blob,
          headers: { 'Content-Type': 'application/gzip' },
        }),
      ),
  });
}

/** Returns the secret key once after the password is re-entered; the caller keeps it in component state only. */
export function useRevealDesktopSecretKey() {
  const api = useApi();
  return useMutation({
    mutationFn: (password: string) =>
      unwrap(api.POST(`${D}/secret-key/reveal`, { body: { password } })),
  });
}

export function useOpenDesktopFolder() {
  const api = useApi();
  return useMutation({
    mutationFn: (target: DesktopOpenTarget) => unwrap(api.POST(`${D}/open`, { body: { target } })),
  });
}

/** Exchanges a pairing code for a browser device session (cookies set by the response). */
export function usePairDeviceSession() {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: PairSessionRequest) =>
      unwrap(api.POST('/api/device/v1/pair-session', { body })),
    onSuccess: () => {
      // A new principal: drop whatever the anonymous page had cached.
      qc.removeQueries({ queryKey: queryKeys.me });
    },
  });
}

/** Verifies the second factor of the device session just created for a user with TOTP (`totp_required`). */
export function useVerifyDeviceSessionTotp() {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: DeviceSessionTotpRequest) =>
      unwrap(api.POST('/api/device/v1/pair-session/totp', { body })),
    onSuccess: () => {
      qc.removeQueries({ queryKey: queryKeys.me });
    },
  });
}
