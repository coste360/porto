import { SearchIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';

import {
  useCustomers,
  useMergeCustomers,
  type Customer,
  type CustomerSummary,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { InputGroup, InputGroupAddon, InputGroupInput } from '@porto/shared/ui/input-group';
import { ProblemAlert } from '@porto/shared/ui/patterns';
import { ScrollArea } from '@porto/shared/ui/scroll-area';

import { useCurrentMarina } from '../../../auth/hooks';
import { customerName } from '../lib/format';

const MAX_SOURCES = 10;

export type MergeDialogProps = {
  target: Customer;
  suggestions: CustomerSummary[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onMerged: () => void;
};

/** Merge duplicates into the current customer (the target): search, pick up to 10 sources, confirm. */
export function MergeDialog({
  target,
  suggestions,
  open,
  onOpenChange,
  onMerged,
}: MergeDialogProps) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const [q, setQ] = useState('');
  const [selected, setSelected] = useState<Map<string, CustomerSummary>>(new Map());
  const [confirming, setConfirming] = useState(false);
  const deferred = useDeferredValue(q.trim());
  const search = useCustomers(marinaId, { q: deferred });
  const merge = useMergeCustomers(marinaId);

  const eligible = (c: CustomerSummary) =>
    c.id !== target.id && !c.anonymized_at && !c.merged_into_id;
  const found = deferred
    ? (search.data?.pages.flatMap((p) => p.items) ?? []).filter(eligible)
    : suggestions.filter(eligible);

  const toggle = (c: CustomerSummary, on: boolean) =>
    setSelected((prev) => {
      const next = new Map(prev);
      if (on && next.size < MAX_SOURCES) next.set(c.id, c);
      else next.delete(c.id);
      return next;
    });

  const close = (o: boolean) => {
    if (!o) {
      setSelected(new Map());
      setConfirming(false);
      setQ('');
      merge.reset();
    }
    onOpenChange(o);
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('customers:merge.title')}</DialogTitle>
          <DialogDescription>
            {t('customers:merge.description', { name: customerName(t, target) })}
          </DialogDescription>
        </DialogHeader>
        {merge.isError ? <ProblemAlert error={merge.error} /> : null}
        {confirming ? (
          <div className="flex flex-col gap-2 text-sm">
            <p>{t('customers:merge.confirmText', { count: selected.size })}</p>
            <ul className="list-disc pl-5">
              {[...selected.values()].map((c) => (
                <li key={c.id}>{customerName(t, c)}</li>
              ))}
            </ul>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <Field>
              <FieldLabel htmlFor="merge-search">{t('customers:merge.search')}</FieldLabel>
              <InputGroup>
                <InputGroupAddon>
                  <SearchIcon />
                </InputGroupAddon>
                <InputGroupInput
                  id="merge-search"
                  type="search"
                  value={q}
                  onChange={(e) => setQ(e.target.value)}
                />
              </InputGroup>
            </Field>
            <ScrollArea className="h-56 rounded-md border">
              <ul className="flex flex-col p-2">
                {found.length === 0 ? (
                  <li className="text-muted-foreground p-2 text-sm">{t('customers:merge.none')}</li>
                ) : (
                  found.map((c) => (
                    <li key={c.id}>
                      <Field orientation="horizontal" className="p-2">
                        <Checkbox
                          id={`merge-${c.id}`}
                          checked={selected.has(c.id)}
                          onCheckedChange={(v) => toggle(c, v === true)}
                        />
                        <FieldLabel htmlFor={`merge-${c.id}`} className="flex flex-col items-start">
                          <span>{customerName(t, c)}</span>
                          <span className="text-muted-foreground font-mono text-xs">
                            {c.fiscal_code ?? c.vat_number ?? c.primary_email ?? ''}
                          </span>
                        </FieldLabel>
                      </Field>
                    </li>
                  ))
                )}
              </ul>
            </ScrollArea>
            <p className="text-muted-foreground text-xs">
              {t('customers:merge.selected', { count: selected.size, max: MAX_SOURCES })}
            </p>
          </div>
        )}
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => (confirming ? setConfirming(false) : close(false))}
          >
            {confirming ? t('common:actions.back') : t('common:actions.cancel')}
          </Button>
          {confirming ? (
            <Button
              variant="destructive"
              disabled={merge.isPending}
              onClick={() =>
                merge.mutate(
                  { targetId: target.id, version: target.version, sourceIds: [...selected.keys()] },
                  {
                    onSuccess: () => {
                      close(false);
                      onMerged();
                    },
                  },
                )
              }
            >
              {t('customers:merge.confirm')}
            </Button>
          ) : (
            <Button disabled={selected.size === 0} onClick={() => setConfirming(true)}>
              {t('common:actions.continue')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
