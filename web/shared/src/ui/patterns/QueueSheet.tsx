import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@porto/shared/ui/drawer';

import { PROBLEM_TYPES } from '../../api/problem';
import { useT } from '../../i18n/useT';
import { useOfflineSession, useOnlineStatus, useQueue, type QueuedOp } from '../../offline/index';

export type QueueSheetProps = {
  marinaId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const NEEDS_USER: ReadonlySet<QueuedOp['state']> = new Set(['conflict', 'blocked', 'expired']);

function stateVariant(op: QueuedOp): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (op.state === 'conflict' || op.state === 'expired') return 'destructive';
  if (op.state === 'done') return 'outline';
  return 'secondary';
}

function problemKey(type: string | undefined): string {
  return type && (PROBLEM_TYPES as readonly string[]).includes(type)
    ? `errors:${type}`
    : 'errors:conflict';
}

/** The new berth of an allocation_changed conflict, when the server named one. */
function currentBerth(op: QueuedOp): { id: string; code: string } | null {
  if (op.problem?.type !== 'allocation_changed') return null;
  const d = op.problem.detail as
    { current_resource_id?: unknown; current_resource_code?: unknown } | undefined;
  return typeof d?.current_resource_id === 'string'
    ? {
        id: d.current_resource_id,
        code: typeof d.current_resource_code === 'string' ? d.current_resource_code : '',
      }
    : null;
}

/**
 * The offline queue of a marina (m6b-plan §4.3): every op with its state and, for a refusal, the server's problem;
 * the user decides: Scarta, or Riprova come nuova operazione (new key; for a moved allocation it accepts the new
 * berth). Nothing is resolved automatically. Ops of another user on this device can only be discarded.
 */
export function QueueSheet({ marinaId, open, onOpenChange }: QueueSheetProps) {
  const { t } = useT();
  const queue = useQueue(marinaId);
  const { online } = useOnlineStatus();
  const session = useOfflineSession();
  const others = queue.ops.some((o) => o.userId !== session.userId && o.state !== 'done');
  return (
    <Drawer open={open} onOpenChange={onOpenChange}>
      <DrawerContent data-testid="queue-sheet">
        <DrawerHeader>
          <DrawerTitle>{t('offline:queue.title')}</DrawerTitle>
          <DrawerDescription>
            {session.authRequired
              ? t('offline:signInAgain')
              : queue.pending === 1
                ? t('offline:status.pending_one', { count: 1 })
                : t('offline:status.pending_other', { count: queue.pending })}
          </DrawerDescription>
        </DrawerHeader>
        <div className="flex max-h-96 flex-col gap-2 overflow-y-auto px-4">
          {queue.ops.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">
              {t('offline:queue.empty')}
            </p>
          ) : null}
          {others ? (
            <p className="text-sm text-muted-foreground">{t('offline:otherUserOps')}</p>
          ) : null}
          {queue.ops.map((op) => {
            const mine = op.userId === session.userId;
            const berth = currentBerth(op);
            return (
              <div
                key={op.id}
                data-testid="queue-op"
                data-state={op.state}
                data-kind={op.kind}
                className="flex flex-col gap-2 rounded-md border p-3"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm font-medium">{t(op.label.key, op.label.params)}</span>
                  <Badge variant={stateVariant(op)}>{t(`offline:queue.state.${op.state}`)}</Badge>
                </div>
                {op.state === 'conflict' ? (
                  <p className="text-sm text-destructive" data-testid="queue-op-problem">
                    {t(problemKey(op.problem?.type))}
                    {berth?.code ? <span className="ml-1 font-medium">{berth.code}</span> : null}
                  </p>
                ) : null}
                {NEEDS_USER.has(op.state) || !mine ? (
                  <div className="flex flex-wrap gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      className="min-h-11"
                      onClick={() => void queue.discard(op.id)}
                    >
                      {t('offline:queue.discard')}
                    </Button>
                    {mine && (op.state === 'conflict' || op.state === 'expired') ? (
                      <Button
                        size="sm"
                        className="min-h-11"
                        onClick={() =>
                          void queue.retryAsNew(
                            op.id,
                            berth ? { expected_resource_id: berth.id } : undefined,
                          )
                        }
                      >
                        {t('offline:queue.retryAsNew')}
                      </Button>
                    ) : null}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
        <DrawerFooter>
          <Button
            className="min-h-11"
            disabled={!online || queue.pending === 0 || session.authRequired}
            onClick={() => queue.sendNow()}
          >
            {t('offline:queue.sendNow')}
          </Button>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  );
}
