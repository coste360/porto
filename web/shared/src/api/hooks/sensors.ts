// Hooks of the M12 sensors (m12-plan §9, §10, §16.1; owner RECONCILE): devices, gateways, sources and settings
// (configuration, harbor.configure), the Sensori map, the alerts with their actions and the histories. Keys live
// under queryKeys.sensors(marinaId), invalidated by sensor.changed (narrowed by map, debounced). Every write is
// online; mutations invalidate the whole sensors subtree (and the day views, whose anomalies read the sensors).
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type SensorKind = S['SensorKind'];
export type SensorResourceType = S['SensorResourceType'];
export type SensorPhysicalState = S['SensorPhysicalState'];
export type SensorHealth = S['SensorHealth'];
export type SensorKeyStatus = S['SensorKeyStatus'];
export type SensorExpectation = S['SensorExpectation'];
export type SensorDevice = S['SensorDevice'];
export type SensorDeviceInput = S['SensorDeviceInput'];
export type SensorDevicePatch = S['SensorDevicePatch'];
export type SensorDeviceWithKey = S['SensorDeviceWithKey'];
export type SensorGateway = S['SensorGateway'];
export type SensorGatewayInput = S['SensorGatewayInput'];
export type SensorGatewayPatch = S['SensorGatewayPatch'];
export type SensorGatewayWithKey = S['SensorGatewayWithKey'];
export type SensorGatewayKind = S['SensorGatewayKind'];
export type SensorSource = S['SensorSource'];
export type SensorSourceInput = S['SensorSourceInput'];
export type SensorSourcePatch = S['SensorSourcePatch'];
export type SensorSourceDriver = S['SensorSourceDriver'];
export type SensorSourcePollResult = S['SensorSourcePollResult'];
export type SensorSettings = S['SensorSettings'];
export type SensorSettingsInput = S['SensorSettingsInput'];
export type SensorMap = S['SensorMap'];
export type SensorMapEntry = S['SensorMapEntry'];
export type SensorAlert = S['SensorAlert'];
export type SensorAlertKind = S['SensorAlertKind'];
export type SensorAlertStatus = S['SensorAlertStatus'];
export type SensorAlertAction = S['SensorAlertAction'];
export type SensorAlertActionInput = S['SensorAlertActionInput'];
export type SensorHistory = S['SensorHistory'];
export type SensorNight = S['SensorNight'];
export type SensorStateChange = S['SensorStateChange'];
export type SensorReadingRow = S['SensorReadingRow'];
export type SensorContractual = S['SensorContractual'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

function invalidate(qc: QueryClient, marinaId: string) {
  void qc.invalidateQueries({ queryKey: queryKeys.sensors(marinaId) });
  void qc.invalidateQueries({ queryKey: queryKeys.dayView(marinaId) });
}

// ---- devices ----

/** Devices of the marina (map.view), at most 200 (one page). */
export function useSensorDevices(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.sensorDevices(marinaId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/sensors/devices`, { params: { path: { marinaId }, query: { limit: 200 } } }),
      ),
    enabled,
  });
}

/** Creates a device; the response carries its key once. */
export function useCreateSensorDevice(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SensorDeviceInput) =>
      unwrap(
        api.POST(`${M}/sensors/devices`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateSensorDevice(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: SensorDevicePatch }) =>
      unwrap(
        api.PATCH(`${M}/sensors/devices/{sensorDeviceId}`, {
          params: {
            path: { marinaId, sensorDeviceId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Rotates the key of a device (the previous one verifies for grace_hours); the new key is returned once. */
export function useRotateSensorDeviceKey(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; graceHours: number }) =>
      unwrap(
        api.POST(`${M}/sensors/devices/{sensorDeviceId}/rotate-key`, {
          params: {
            path: { marinaId, sensorDeviceId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { grace_hours: v.graceHours },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useRevokeSensorDeviceKey(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.POST(`${M}/sensors/devices/{sensorDeviceId}/revoke-key`, {
          params: {
            path: { marinaId, sensorDeviceId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- gateways ----

export function useSensorGateways(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.sensorGateways(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/sensors/gateways`, { params: { path: { marinaId } } })),
    enabled,
  });
}

export function useCreateSensorGateway(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SensorGatewayInput) =>
      unwrap(
        api.POST(`${M}/sensors/gateways`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateSensorGateway(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: SensorGatewayPatch }) =>
      unwrap(
        api.PATCH(`${M}/sensors/gateways/{sensorGatewayId}`, {
          params: {
            path: { marinaId, sensorGatewayId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useRotateSensorGatewayKey(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; graceHours: number }) =>
      unwrap(
        api.POST(`${M}/sensors/gateways/{sensorGatewayId}/rotate-key`, {
          params: {
            path: { marinaId, sensorGatewayId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { grace_hours: v.graceHours },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useRevokeSensorGatewayKey(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.POST(`${M}/sensors/gateways/{sensorGatewayId}/revoke-key`, {
          params: {
            path: { marinaId, sensorGatewayId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- sources ----

/** Polled sources and the registered drivers with their config keys. */
export function useSensorSources(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.sensorSources(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/sensors/sources`, { params: { path: { marinaId } } })),
    enabled,
  });
}

export function useCreateSensorSource(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SensorSourceInput) =>
      unwrap(
        api.POST(`${M}/sensors/sources`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateSensorSource(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: SensorSourcePatch }) =>
      unwrap(
        api.PATCH(`${M}/sensors/sources/{sensorSourceId}`, {
          params: {
            path: { marinaId, sensorSourceId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** "Interroga ora": polls a source once. */
export function usePollSensorSource(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      unwrap(
        api.POST(`${M}/sensors/sources/{sensorSourceId}/poll`, {
          params: { path: { marinaId, sensorSourceId: id } },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- settings ----

/** Sensor settings (map.view). */
export function useSensorSettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.sensorSettings(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/sensors/settings`, { params: { path: { marinaId } } })),
  });
}

/** Saves the settings (harbor.configure; version 0 for the first save). */
export function usePutSensorSettings(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { version: number; body: SensorSettingsInput }) =>
      unwrap(
        api.PUT(`${M}/sensors/settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- map, alerts, history ----

/** Context Sensori of a map (map.view; date = a past night). */
export function useSensorMap(marinaId: string, mapId?: string, date?: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.sensorMap(marinaId, mapId, date),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/sensors/map`, {
          params: { path: { marinaId }, query: { map_id: mapId, date } },
        }),
      ),
    enabled,
  });
}

export type SensorAlertFilter = {
  status?: SensorAlertStatus;
  kind?: SensorAlertKind;
  map_id?: string;
  device_id?: string;
};

/** Sensor alerts and proposals, newest first (map.view; default the open ones). */
export function useSensorAlerts(
  marinaId: string,
  filter: SensorAlertFilter | SensorAlertStatus = 'open',
  enabled = true,
) {
  const api = useApi();
  const f: SensorAlertFilter = typeof filter === 'string' ? { status: filter } : filter;
  return useQuery({
    queryKey: queryKeys.sensorAlerts(marinaId, { ...f }),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/sensors/alerts`, {
          params: { path: { marinaId }, query: { ...f, limit: 200 } },
        }),
      ),
    enabled,
  });
}

/** Decides an alert (If-Match; the permission depends on the action, the server checks it). */
export function useActOnSensorAlert(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: SensorAlertActionInput }) =>
      unwrap(
        api.POST(`${M}/sensors/alerts/{sensorAlertId}/actions`, {
          params: {
            path: { marinaId, sensorAlertId: v.id },
            header: { 'If-Match': ifMatch(v.version), 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      ),
    onSettled: () => invalidate(qc, marinaId),
  });
}

export type SensorHistoryQuery = {
  resource_type?: SensorResourceType;
  resource_id?: string;
  from?: string;
  to?: string;
  include_readings?: boolean;
};

/** Physical history of a resource (map.view; at most 31 days). */
export function useSensorHistory(marinaId: string, q: SensorHistoryQuery, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.sensorHistory(marinaId, { ...q }),
    queryFn: () =>
      unwrap(api.GET(`${M}/sensors/history`, { params: { path: { marinaId }, query: q } })),
    enabled: enabled && !!q.resource_id && !!q.resource_type,
  });
}
