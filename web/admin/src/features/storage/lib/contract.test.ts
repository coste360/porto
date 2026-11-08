import { describe, expect, it } from 'vitest';

import {
  emptyStorageForm,
  storageCreateBody,
  storageFormError,
  storagePatchBody,
  storagePreviewBody,
  usesMarinaCradle,
} from './contract';

describe('storage contract form', () => {
  const ok = {
    ...emptyStorageForm('2026-10-01', 'area-1'),
    customerId: 'c1',
    vesselId: 'v1',
  };

  it('validates in order and accepts the automatic position', () => {
    expect(storageFormError(emptyStorageForm('2026-10-01'))).toBe(
      'storage:contract.errors.customer',
    );
    expect(storageFormError(ok)).toBeNull();
    expect(storageFormError({ ...ok, term: 'period' })).toBe('storage:contract.errors.endsOn');
    expect(storageFormError({ ...ok, autoPlace: false })).toBe('storage:contract.errors.placement');
    expect(storageFormError({ ...ok, haulOutOn: '2026-10-10', launchOn: '2026-10-01' })).toBe(
      'storage:contract.errors.launch',
    );
  });

  it('builds the create body without placement when automatic', () => {
    expect(storageCreateBody(ok)).toEqual({
      term: 'seasonal',
      customer_id: 'c1',
      vessel_id: 'v1',
      starts_on: '2026-10-01',
      area_id: 'area-1',
      support: 'cradle',
      support_owner: 'own',
      activate: false,
    });
    expect(
      storageCreateBody({ ...ok, autoPlace: false, x: 300, y: 1200, rotation: 900 }).placement,
    ).toEqual({
      x_cm: 300,
      y_cm: 1200,
      rotation_dd: 900,
    });
  });

  it('clears dates with null in the patch', () => {
    expect(
      storagePatchBody({
        support: 'trailer',
        supportOwner: 'marina',
        haulOutOn: null,
        launchOn: '2027-04-01',
        notesHandling: ' x ',
      }),
    ).toEqual({
      support: 'trailer',
      support_owner: 'marina',
      haul_out_on: null,
      launch_on: '2027-04-01',
      notes_handling: 'x',
    });
  });

  it('sends the marina cradle only with a marina-owned cradle', () => {
    const marina = { ...ok, supportOwner: 'marina' as const, cradleId: 'cr-1' };
    expect(usesMarinaCradle(marina)).toBe(true);
    expect(storageCreateBody(marina).cradle_resource_id).toBe('cr-1');
    expect(
      storageCreateBody({ ...marina, supportOwner: 'own' }).cradle_resource_id,
    ).toBeUndefined();
    expect(storageCreateBody({ ...marina, support: 'trailer' }).cradle_resource_id).toBeUndefined();
  });

  it('builds the price preview request only with every input and an end date', () => {
    expect(storagePreviewBody(ok)).toBeNull();
    expect(storagePreviewBody({ ...ok, endsOn: '2026-09-01' })).toBeNull();
    expect(storagePreviewBody({ ...ok, endsOn: '2027-04-01' })).toEqual({
      contract_type: 'storage',
      arrival: '2026-10-01',
      departure: '2027-04-01',
      vessel_id: 'v1',
      customer_id: 'c1',
      resource: { type: 'dynamic_area', id: 'area-1' },
    });
  });
});
