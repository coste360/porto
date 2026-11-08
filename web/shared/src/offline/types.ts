// Types and errors of the offline module (m6b-plan §4.1). Public through index.ts.
import type { OfflineOpKind } from './allowlist';

export type OpState =
  'pending' | 'sending' | 'retrying' | 'conflict' | 'blocked' | 'expired' | 'done';

export interface QueuedOp {
  id: string;
  kind: OfflineOpKind;
  /** Idempotency-Key, fixed at enqueue and reused on every attempt. */
  key: string;
  userId: string;
  marinaId: string;
  /** Client clock, RFC 3339; sent as Porto-Offline-Queued-At. */
  createdAt: string;
  /** Last state change (client clock); done ops are pruned an hour after it. */
  updatedAt?: string;
  /** Ordering key: vessel:<id> (presences and photos), charge:<id> (payments). */
  lane: string;
  /** e.g. { opId, bind: { presenceId: 'presence.id' } }: path parameters taken from the parent's stored result. */
  dependsOn?: { opId: string; bind: Record<string, string> };
  request: {
    operationId: string;
    path: string;
    body?: unknown;
    blobId?: string;
    contentType?: string;
  };
  state: OpState;
  attempts: number;
  nextAttemptAt?: string;
  /** True when the op was enqueued while the device was offline: every attempt is an offline replay. */
  queuedOffline?: boolean;
  problem?: { status: number; type: string; detail?: unknown };
  result?: unknown;
  /** i18n label for the queue sheet. */
  label: { key: string; params: Record<string, string> };
}

/** What a caller enqueues; the queue fills id, key, createdAt, state and attempts. */
export type NewOp = Pick<
  QueuedOp,
  'kind' | 'userId' | 'marinaId' | 'lane' | 'dependsOn' | 'request' | 'label'
> & {
  /** Photo bytes of presence.photo (stored in the blobs store until the op is done or discarded). */
  blob?: Blob;
  /**
   * payment.cash only: whether a matched charge is in receipt mode (the payment numbers a commercial document,
   * online only, 29.2 / D6). Not stored with the op; validateNewOp refuses true and unknown (undefined) alike.
   */
  issuesDocument?: boolean;
};

export type OfflineReasonCode = 'allocation' | 'document' | 'online_only';

/** Thrown by the mutation hooks of online-only operations while offline, and by enqueue for an operation that
 * offline may not perform. */
export class OfflineForbiddenError extends Error {
  readonly operationId: string;
  readonly reason: OfflineReasonCode;
  constructor(operationId: string, reason: OfflineReasonCode = 'online_only') {
    super(`offline: ${operationId} is not available offline`);
    this.name = 'OfflineForbiddenError';
    this.operationId = operationId;
    this.reason = reason;
  }

  /** i18n key of the reason (offline:forbidden.<reason>). */
  get i18nKey(): string {
    return `offline:forbidden.${this.reason}`;
  }
}

/** Thrown by enqueue when the device has less than 20 MiB free (offline:quotaFull). */
export class OfflineQuotaError extends Error {
  readonly i18nKey = 'offline:quotaFull';
  constructor() {
    super('offline: storage quota almost full');
    this.name = 'OfflineQuotaError';
  }
}

/**
 * Rejection of a queue-backed mutation hook whose operation is still queued (offline, or the server did not
 * answer in time): not a failure, the queue sends it later. `op` is the queued operation.
 */
export class OfflineQueuedError extends Error {
  readonly op: QueuedOp;
  readonly i18nKey = 'offline:queuedBadge';
  constructor(op: QueuedOp) {
    super('offline: operation queued');
    this.name = 'OfflineQueuedError';
    this.op = op;
  }
}

/** True for the rejection of a queue-backed hook whose operation is queued (treat it as accepted). */
export function isQueuedOffline(err: unknown): err is OfflineQueuedError {
  return err instanceof OfflineQueuedError;
}
