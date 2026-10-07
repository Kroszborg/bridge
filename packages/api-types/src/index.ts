// Generated types live in ./schema.d.ts. Regenerate with `pnpm api:generate`
// after changing the Go API; never edit schema.d.ts by hand.
export type { components, operations, paths } from './schema';

import type { components } from './schema';

type Schemas = components['schemas'];

export type ApiError = Schemas['APIError'];
export type ErrorBody = Schemas['ErrorBody'];
export type User = Schemas['User'];
export type Organization = Schemas['Organization'];
export type Project = Schemas['Project'];
export type ApiKey = Schemas['APIKey'];
export type CreatedApiKey = Schemas['CreatedAPIKey'];
export type MeResponse = Schemas['MeResponse'];
export type WhoAmI = Schemas['WhoAmI'];
export type Device = Schemas['Device'];
export type PairingToken = Schemas['PairingToken'];
export type WakeResult = Schemas['WakeResult'];
export type Message = Schemas['Message'];
export type MessageDetail = Schemas['MessageDetail'];
export type MessageEvent = Schemas['MessageEvent'];
export type MessageList = Schemas['MessageList'];
export type Usage = Schemas['Usage'];
export type UsagePeriod = Schemas['UsagePeriod'];
export type DeviceSim = Schemas['DeviceSIM'];
export type WebhookEndpoint = Schemas['WebhookEndpoint'];
export type CreatedWebhookEndpoint = Schemas['CreatedWebhookEndpoint'];
export type WebhookDelivery = Schemas['WebhookDelivery'];
export type WebhookSecret = Schemas['WebhookSecret'];
export type WebhookTestResult = Schemas['WebhookTestResult'];
export type WebhookEventType = WebhookEndpoint['events'][number];
export type RequestLog = Schemas['RequestLog'];
export type RequestLogList = Schemas['RequestLogList'];
export type UsageHistory = Schemas['UsageHistory'];
export type UsageDay = Schemas['UsageDay'];
export type DeviceUsage = Schemas['DeviceUsage'];
export type Member = Schemas['Member'];
export type Invite = Schemas['Invite'];
export type CreatedInvite = Schemas['CreatedInvite'];
export type InvitePreview = Schemas['InvitePreview'];
export type AuditEntry = Schemas['AuditEntry'];
export type AuditList = Schemas['AuditList'];
export type Session = Schemas['Session'];
export type StatusPage = Schemas['StatusPage'];
export type StatusComponent = Schemas['StatusComponent'];
export type StatusDay = Schemas['StatusDay'];
export type SystemHealth = Schemas['SystemHealth'];
export type Verification = Schemas['Verification'];
export type VerificationList = Schemas['VerificationList'];
export type VerificationStats = Schemas['VerificationStats'];
export type VerifyResult = Schemas['VerifyResult'];
export type OtpSettings = Schemas['OTPSettings'];
export type OtpSettingsInput = Schemas['OTPSettingsInput'];
