// Small building blocks shared by the M4 pages (contracts, rights, documents): money and date cells, status
// badges from the docsEnums namespace, a section card, a wizard step bar, a reason dialog, CSV/PDF downloads and
// "load more" for infinite lists. Only shadcn components and theme tokens.
import type { UseInfiniteQueryResult } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';

import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@porto/shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Textarea } from '@porto/shared/ui/textarea';

import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';

export { DeskProblemAlert as M4ProblemAlert };

export type BadgeVariant = 'default' | 'secondary' | 'outline' | 'destructive';

export function Money({ cents }: { cents: number | null | undefined }) {
  const { i18n } = useT();
  if (cents === null || cents === undefined) return null;
  return <span className="tabular-nums">{formatCents(cents, 'EUR', i18n.language)}</span>;
}

export function IsoDate({ date }: { date: string | null | undefined }) {
  return date ? <span className="tabular-nums">{formatIsoDate(date)}</span> : null;
}

/** Badge of an enum value: `docsEnums:<group>.<value>`. */
export function EnumBadge({
  group,
  value,
  variant = 'secondary',
}: {
  group: string;
  value: string;
  variant?: BadgeVariant;
}) {
  const { t } = useT();
  return <Badge variant={variant}>{t(`docsEnums:${group}.${value}`)}</Badge>;
}

export function Section({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {description ? <CardDescription>{description}</CardDescription> : null}
        {action ? <CardAction>{action}</CardAction> : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">{children}</CardContent>
    </Card>
  );
}

/** Step indicator of a wizard (composed from buttons: shadcn has no stepper). */
export function StepBar<S extends string>({
  steps,
  current,
  label,
  labelOf,
  onSelect,
}: {
  steps: readonly S[];
  current: S;
  label: string;
  labelOf: (s: S) => string;
  onSelect?: (s: S) => void;
}) {
  const index = steps.indexOf(current);
  return (
    <nav aria-label={label}>
      <ol className="flex flex-wrap gap-2">
        {steps.map((s, i) => (
          <li key={s}>
            <Button
              type="button"
              size="sm"
              variant={i === index ? 'default' : i < index ? 'secondary' : 'outline'}
              aria-current={i === index ? 'step' : undefined}
              disabled={!onSelect || i > index}
              onClick={() => onSelect?.(s)}
            >
              {`${i + 1}. ${labelOf(s)}`}
            </Button>
          </li>
        ))}
      </ol>
    </nav>
  );
}

/** Dialog asking for a reason (required or optional) before a destructive or recorded action. */
export function ReasonDialog({
  open,
  title,
  description,
  confirmLabel,
  required = false,
  pending,
  error,
  destructive = false,
  onClose,
  onConfirm,
}: {
  open: boolean;
  title: string;
  description?: string;
  confirmLabel: string;
  required?: boolean;
  pending?: boolean;
  error?: unknown;
  destructive?: boolean;
  onClose: () => void;
  onConfirm: (reason: string) => void;
}) {
  const { t } = useT();
  const [reason, setReason] = useState('');
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="m4-reason">{t('contractsM4:common.reason')}</FieldLabel>
          <Textarea
            id="m4-reason"
            value={reason}
            maxLength={500}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <DeskProblemAlert error={error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            variant={destructive ? 'destructive' : 'default'}
            disabled={pending || (required && !reason.trim())}
            onClick={() => onConfirm(reason.trim())}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function LoadMore({
  query,
}: {
  query: Pick<UseInfiniteQueryResult, 'hasNextPage' | 'isFetchingNextPage' | 'fetchNextPage'>;
}) {
  const { t } = useT();
  if (!query.hasNextPage) return null;
  return (
    <div>
      <Button
        variant="outline"
        disabled={query.isFetchingNextPage}
        onClick={() => void query.fetchNextPage()}
      >
        {t('contractsM4:common.loadMore')}
      </Button>
    </div>
  );
}

/** A labelled value of a details list. */
export function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

export function DetailGrid({ children }: { children: ReactNode }) {
  return <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{children}</dl>;
}

/** Page frame shared by the M4 pages. */
export function PageFrame({ children }: { children: ReactNode }) {
  return <div className="mx-auto flex w-full max-w-6xl flex-col gap-6">{children}</div>;
}

const ALL = '__all__';

/** Select over an enum (labels `docsEnums:<group>.<value>` unless `labelOf`), with an optional "all" entry. */
export function EnumSelect<V extends string>({
  id,
  label,
  values,
  value,
  onChange,
  group,
  labelOf,
  allLabel,
}: {
  id: string;
  label: string;
  values: readonly V[];
  value: V | '';
  onChange: (v: V | '') => void;
  group?: string;
  labelOf?: (v: V) => string;
  allLabel?: string;
}) {
  const { t } = useT();
  const text = (v: V) => (labelOf ? labelOf(v) : t(`docsEnums:${group}.${v}`));
  return (
    <Field className="w-auto min-w-40">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select
        value={value || (allLabel ? ALL : '')}
        onValueChange={(v) => onChange(v === ALL ? '' : (v as V))}
      >
        <SelectTrigger id={id}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {allLabel ? <SelectItem value={ALL}>{allLabel}</SelectItem> : null}
          {values.map((v) => (
            <SelectItem key={v} value={v}>
              {text(v)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}
