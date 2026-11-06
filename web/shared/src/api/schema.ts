/**
 * Single switch point for the API types, generated from api/openapi.yaml by `npm run gen`
 * (openapi-typescript). Never edit the generated file; change the contract and regenerate.
 */
export type { paths, components } from '../../api/generated/schema';

import type { components } from '../../api/generated/schema';

export type Schemas = components['schemas'];
export type Me = Schemas['Me'];
export type MeMarina = Me['marinas'][number];
export type Role = Schemas['Role'];
export type Permission = Schemas['Permission'];
export type Marina = Schemas['Marina'];
export type Organization = Schemas['Organization'];
export type Member = Schemas['Member'];
export type ApiKey = Schemas['ApiKey'];
export type Device = Schemas['Device'];
export type PairingCode = Schemas['PairingCode'];
export type WebhookEndpoint = Schemas['WebhookEndpoint'];
export type WebhookDelivery = Schemas['WebhookDelivery'];
export type EventType = Schemas['EventType'];
export type AuditEntry = Schemas['AuditEntry'];
export type PublicMarina = Schemas['PublicMarina'];
export type Problem = Schemas['Problem'];

// M1 harbour (m1-plan §4.3)
export type MapKind = Schemas['MapKind'];
export type MapSummary = Schemas['MapSummary'];
export type MapDocument = Schemas['MapDocument'];
export type MapMeta = Schemas['MapMeta'];
export type MapElements = Schemas['MapElements'];
export type MapSaveRequest = Schemas['MapSaveRequest'];
export type MapSaveResult = Schemas['MapSaveResult'];
export type MapIssue = Schemas['MapIssue'];
export type MapValidationReport = Schemas['MapValidationReport'];
export type MapExport = Schemas['MapExport'];
export type MapBackground = Schemas['MapBackground'];
export type MapBackgroundFile = Schemas['MapBackgroundFile'];
export type Georef = Schemas['Georef'];
export type ControlPoint = Schemas['ControlPoint'];
export type Point = Schemas['Point'];
export type Polygon = Schemas['Polygon'];
export type I18nText = Schemas['I18nText'];
export type Structure = Schemas['Structure'];
export type Berth = Schemas['Berth'];
export type DynamicArea = Schemas['DynamicArea'];
export type LandResource = Schemas['LandResource'];
export type Pedestal = Schemas['Pedestal'];
export type Decoration = Schemas['Decoration'];
export type GeneratorParams = Schemas['GeneratorParams'];
export type GenerateRequest = Schemas['GenerateRequest'];
export type GenerateResult = Schemas['GenerateResult'];
export type BerthSummary = Schemas['BerthSummary'];
export type BerthGroup = Schemas['BerthGroup'];
export type HarborSettings = Schemas['HarborSettings'];
export type PriceClassRule = Schemas['PriceClassRule'];
export type BerthImportRequest = Schemas['BerthImportRequest'];
export type BerthImportReport = Schemas['BerthImportReport'];

// M1 registry (m1-plan §4.4)
export type Customer = Schemas['Customer'];
export type CustomerInput = Schemas['CustomerInput'];
export type CustomerPatch = Schemas['CustomerPatch'];
export type CustomerSummary = Schemas['CustomerSummary'];
export type CustomerCreated = Schemas['CustomerCreated'];
export type CustomerOverview = Schemas['CustomerOverview'];
export type CustomerExport = Schemas['CustomerExport'];
export type Consent = Schemas['Consent'];
export type Vessel = Schemas['Vessel'];
export type VesselInput = Schemas['VesselInput'];
export type VesselPatch = Schemas['VesselPatch'];
export type VesselSummary = Schemas['VesselSummary'];
export type VesselParty = Schemas['VesselParty'];
export type Document = Schemas['Document'];
export type DocumentInput = Schemas['DocumentInput'];
export type DocumentPatch = Schemas['DocumentPatch'];
export type DocumentType = Schemas['DocumentType'];
export type Deadline = Schemas['Deadline'];
export type DeadlinePolicy = Schemas['DeadlinePolicy'];
export type DeadlineKind = Schemas['DeadlineKind'];
