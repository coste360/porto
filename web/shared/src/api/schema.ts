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
