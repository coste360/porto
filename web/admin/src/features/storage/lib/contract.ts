import type {
  Schemas,
  StorageContractCreate,
  StorageContractPatch,
  StorageSupport,
  StorageSupportOwner,
} from '@porto/shared/api';

// Storage contract form (m9-plan §3.2, §12.1): state in integer cm and ISO dates, validation as i18n keys, the
// request body built only from a valid form.

export type StorageForm = {
  term: 'annual' | 'seasonal' | 'monthly' | 'period';
  customerId: string | null;
  vesselId: string | null;
  startsOn: string | null;
  endsOn: string | null;
  areaId: string | null;
  autoPlace: boolean;
  x: number | null;
  y: number | null;
  rotation: 0 | 900 | 1800 | 2700;
  support: StorageSupport;
  supportOwner: StorageSupportOwner;
  /** Marina cradle (land resource of kind cradle) for the whole period; only with a marina-owned cradle. */
  cradleId: string | null;
  haulOutOn: string | null;
  launchOn: string | null;
  notesHandling: string;
  activate: boolean;
};

export function emptyStorageForm(today: string, areaId?: string): StorageForm {
  return {
    term: 'seasonal',
    customerId: null,
    vesselId: null,
    startsOn: today,
    endsOn: null,
    areaId: areaId ?? null,
    autoPlace: true,
    x: null,
    y: null,
    rotation: 0,
    support: 'cradle',
    supportOwner: 'own',
    cradleId: null,
    haulOutOn: null,
    launchOn: null,
    notesHandling: '',
    activate: false,
  };
}

/** The first validation error (an i18n key of storage:contract.errors), or null. */
export function storageFormError(f: StorageForm): string | null {
  if (!f.customerId) return 'storage:contract.errors.customer';
  if (!f.vesselId) return 'storage:contract.errors.vessel';
  if (!f.areaId) return 'storage:contract.errors.area';
  if (!f.startsOn) return 'storage:contract.errors.startsOn';
  if (f.term === 'period' && !f.endsOn) return 'storage:contract.errors.endsOn';
  if (f.endsOn && f.endsOn <= f.startsOn) return 'storage:contract.errors.endsOn';
  if (!f.autoPlace && (f.x === null || f.y === null || f.x < 0 || f.y < 0))
    return 'storage:contract.errors.placement';
  if (f.haulOutOn && f.launchOn && f.launchOn < f.haulOutOn)
    return 'storage:contract.errors.launch';
  if (f.notesHandling.length > 500) return 'storage:contract.errors.notes';
  return null;
}

/** The create body of a valid form. */
export function storageCreateBody(f: StorageForm): StorageContractCreate {
  return {
    term: f.term,
    customer_id: f.customerId!,
    vessel_id: f.vesselId!,
    starts_on: f.startsOn!,
    ...(f.endsOn ? { ends_on: f.endsOn } : {}),
    area_id: f.areaId!,
    ...(!f.autoPlace ? { placement: { x_cm: f.x!, y_cm: f.y!, rotation_dd: f.rotation } } : {}),
    support: f.support,
    support_owner: f.supportOwner,
    ...(usesMarinaCradle(f) && f.cradleId ? { cradle_resource_id: f.cradleId } : {}),
    ...(f.haulOutOn ? { haul_out_on: f.haulOutOn } : {}),
    ...(f.launchOn ? { launch_on: f.launchOn } : {}),
    ...(f.notesHandling.trim() ? { notes_handling: f.notesHandling.trim() } : {}),
    activate: f.activate,
  };
}

/** Whether the form asks for a marina cradle (the cradle picker is shown). */
export function usesMarinaCradle(f: Pick<StorageForm, 'support' | 'supportOwner'>): boolean {
  return f.support === 'cradle' && f.supportOwner === 'marina';
}

/**
 * The price preview request of the form (POST pricing/simulate, contract type storage), or null while customer,
 * vessel, area or the dates are missing. Without an explicit end the server derives it from the term, so the
 * preview waits for an end date. The web never computes the price.
 */
export function storagePreviewBody(f: StorageForm): Schemas['PricingSimulateRequest'] | null {
  if (!f.customerId || !f.vesselId || !f.areaId || !f.startsOn || !f.endsOn) return null;
  if (f.endsOn <= f.startsOn) return null;
  return {
    contract_type: 'storage',
    arrival: f.startsOn,
    departure: f.endsOn,
    vessel_id: f.vesselId,
    customer_id: f.customerId,
    resource: { type: 'dynamic_area', id: f.areaId },
  };
}

/** The PATCH body of the details edit: dates cleared explicitly with null. */
export function storagePatchBody(d: {
  support: StorageSupport;
  supportOwner: StorageSupportOwner;
  haulOutOn: string | null;
  launchOn: string | null;
  notesHandling: string;
}): StorageContractPatch {
  return {
    support: d.support,
    support_owner: d.supportOwner,
    haul_out_on: d.haulOutOn,
    launch_on: d.launchOn,
    notes_handling: d.notesHandling.trim(),
  };
}
