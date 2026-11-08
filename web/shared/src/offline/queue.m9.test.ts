import { validateNewOp } from './queue';
import { M, USER } from './testkit';
import { OfflineForbiddenError, type NewOp } from './types';

// M9 offline kinds (m9-plan §10, INV-71): readings, field reports and outcomes, with their photo uploads.

function reading(body: Record<string, unknown>): NewOp {
  return {
    kind: 'utility.reading',
    userId: USER,
    marinaId: M,
    lane: 'meter:m1',
    request: {
      operationId: 'recordMeterReading',
      path: `/api/admin/v1/marinas/${M}/meter-readings`,
      body,
    },
    label: { key: 'utilities:queue.reading', params: { code: 'A1-E' } },
  };
}

function upload(kind: 'utility.reading' | 'maintenance.report', blob?: Blob): NewOp {
  const operationId =
    kind === 'utility.reading' ? 'uploadMeterReadingPhoto' : 'uploadMaintenancePhoto';
  return {
    kind,
    userId: USER,
    marinaId: M,
    lane: 'meter:m1',
    dependsOn: { opId: 'parent', bind: { readingId: 'id' } },
    request: { operationId, path: '/x/{readingId}/photo' },
    label: { key: 'utilities:queue.photo', params: {} },
    ...(blob ? { blob } : {}),
  };
}

const jpeg = () => new Blob([new Uint8Array([0xff, 0xd8, 0xff])], { type: 'image/jpeg' });

describe('validateNewOp: M9 kinds', () => {
  it('accepts readings, reports and outcomes with a client_ref', () => {
    expect(() =>
      validateNewOp(reading({ meter_id: 'm1', value: '12,5', client_ref: 'r1' })),
    ).not.toThrow();
    expect(() =>
      validateNewOp({
        ...reading({}),
        kind: 'maintenance.report',
        request: {
          operationId: 'reportMaintenance',
          path: '/r',
          body: { client_ref: 'x', title: 'Bitta' },
        },
      }),
    ).not.toThrow();
    expect(() =>
      validateNewOp({
        ...reading({}),
        kind: 'maintenance.outcome',
        request: {
          operationId: 'completeMaintenanceTask',
          path: '/c',
          body: { client_ref: 'x', outcome: 'ok' },
        },
      }),
    ).not.toThrow();
    expect(() => validateNewOp(upload('utility.reading', jpeg()))).not.toThrow();
    expect(() => validateNewOp(upload('maintenance.report', jpeg()))).not.toThrow();
  });

  it('refuses ops the server could not deduplicate, uploads without bytes and other operations', () => {
    const bad: NewOp[] = [
      reading({ meter_id: 'm1', value: '12' }),
      reading({ value: '12', client_ref: 'r' }),
      reading({ meter_id: 'm1', client_ref: 'r' }),
      {
        ...reading({}),
        kind: 'maintenance.report',
        request: { operationId: 'reportMaintenance', path: '/r', body: { client_ref: 'x' } },
      },
      {
        ...reading({}),
        kind: 'maintenance.outcome',
        request: { operationId: 'completeMaintenanceTask', path: '/c', body: { outcome: 'ok' } },
      },
      // Scheduling books nights: never offline.
      {
        ...reading({}),
        kind: 'maintenance.outcome',
        request: { operationId: 'scheduleMaintenanceTask', path: '/s', body: { client_ref: 'x' } },
      },
      upload('utility.reading'),
      upload('maintenance.report', new Blob([], { type: 'image/jpeg' })),
      upload('maintenance.report', new Blob(['%PDF'], { type: 'application/pdf' })),
    ];
    for (const op of bad)
      expect(() => validateNewOp(op), op.request.operationId).toThrow(OfflineForbiddenError);
  });
});
