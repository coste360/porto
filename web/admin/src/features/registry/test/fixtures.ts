// Test fixtures of the registry pages: synthetic data only (example.invalid, checksum-valid fake codes).
import type {
  Customer,
  CustomerOverview,
  CustomerSummary,
  Deadline,
  Document,
  Me,
  Vessel,
} from '@porto/shared/api';

import { MARINA_ID, makeMe } from '../../../test/renderApp';

export const API = `/api/admin/v1/marinas/${MARINA_ID}`;

export const REGISTRY_PERMISSIONS: Me['marinas'][number]['permissions'] = [
  'marina.read',
  'marina.configure',
  'customers.read',
  'customers.write',
  'harbor.configure',
  'map.view',
];

export const meWithRegistry = (permissions = REGISTRY_PERMISSIONS) => makeMe({}, permissions);

export const CUSTOMER_ID = '0190a000-0000-7000-8000-00000000c001';
export const DUPLICATE_ID = '0190a000-0000-7000-8000-00000000c002';
export const VESSEL_ID = '0190a000-0000-7000-8000-00000000a001';
export const DOCUMENT_ID = '0190a000-0000-7000-8000-00000000d001';

export const customer: Customer = {
  id: CUSTOMER_ID,
  kind: 'person',
  first_name: 'Mario',
  last_name: 'Rossi',
  company_name: '',
  display_name: 'Mario Rossi',
  fiscal_code: 'RSSMRA85T10A562S',
  vat_number: null,
  sdi_code: null,
  pec: null,
  billing_address: { line1: 'Via del Porto 1', city: 'Genova', country: 'IT' },
  nationality: 'IT',
  locale: 'it',
  emails: [{ label: 'casa', value: 'mario@example.invalid' }],
  phones: [],
  category: 'resident',
  tags: ['vip'],
  payment_terms: null,
  iban_masked: 'IT•• •••• 3456',
  notes: '',
  merged_into_id: null,
  anonymized_at: null,
  archived_at: null,
  version: 3,
  created_at: '2026-01-01T10:00:00Z',
  updated_at: '2026-01-02T10:00:00Z',
};

export const summary = (over: Partial<CustomerSummary> = {}): CustomerSummary => ({
  id: CUSTOMER_ID,
  kind: 'person',
  display_name: 'Mario Rossi',
  fiscal_code: 'RSSMRA85T10A562S',
  vat_number: null,
  category: 'resident',
  tags: ['vip'],
  primary_email: 'mario@example.invalid',
  primary_phone: null,
  vessel_count: 1,
  archived_at: null,
  anonymized_at: null,
  merged_into_id: null,
  ...over,
});

export const document: Document = {
  id: DOCUMENT_ID,
  owner_type: 'vessel',
  customer_id: null,
  vessel_id: VESSEL_ID,
  type: 'insurance_policy',
  number: 'POL-123',
  issuer: 'Assicurazioni Prova',
  issued_on: '2026-01-01',
  expires_on: '2026-11-16',
  has_file: true,
  file_name: 'polizza.pdf',
  content_type: 'application/pdf',
  size_bytes: 1234,
  notes: '',
  version: 1,
  created_at: '2026-01-01T10:00:00Z',
  updated_at: '2026-01-01T10:00:00Z',
};

export const deadline = (over: Partial<Deadline> = {}): Deadline => ({
  id: '0190a000-0000-7000-8000-00000000e001',
  kind: 'insurance',
  source_type: 'document',
  source_id: DOCUMENT_ID,
  customer: { id: CUSTOMER_ID, display_name: 'Mario Rossi' },
  vessel: { id: VESSEL_ID, name: 'Libeccio' },
  document_type: 'insurance_policy',
  due_on: '2026-11-16',
  status: 'open',
  urgency: 'due_soon',
  note: '',
  version: 2,
  ...over,
});

export const overview = (over: Partial<CustomerOverview> = {}): CustomerOverview => ({
  customer,
  vessels: [],
  documents: [],
  deadlines: [],
  consents: [],
  duplicates: [],
  ...over,
});

export const vessel: Vessel = {
  id: VESSEL_ID,
  name: 'Libeccio',
  registration: 'GE1234',
  registry_office: null,
  flag: 'IT',
  home_port: null,
  hull_type: 'monohull_sail',
  model: null,
  builder: null,
  year_built: 2010,
  hull_color: null,
  hull_material: null,
  loa_cm: 1250,
  lwl_cm: null,
  beam_cm: 410,
  draft_cm: 190,
  air_draft_cm: null,
  displacement_kg: null,
  measured_loa_cm: null,
  measured_beam_cm: null,
  measured_draft_cm: null,
  measured_air_draft_cm: null,
  measured_at: null,
  measured_by: null,
  dimensions_source: 'declared',
  effective_loa_cm: 1250,
  effective_beam_cm: 410,
  effective_draft_cm: 190,
  unit_class: 'imbarcazione',
  engines: [],
  fuel: 'diesel',
  power_required_a: 16,
  notes: '',
  parties: [
    { customer_id: CUSTOMER_ID, display_name: 'Mario Rossi', role: 'owner', share_millis: 600 },
    { customer_id: DUPLICATE_ID, display_name: 'Anna Bianchi', role: 'owner', share_millis: 400 },
  ],
  documents: [document],
  archived_at: null,
  version: 5,
  created_at: '2026-01-01T10:00:00Z',
  updated_at: '2026-01-01T10:00:00Z',
};
