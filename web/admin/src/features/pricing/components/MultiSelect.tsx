import { ChevronsUpDownIcon } from 'lucide-react';
import type { ReactNode } from 'react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Label } from '@porto/shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '@porto/shared/ui/popover';

export type MultiSelectOption = { value: string; label: ReactNode };

export type MultiSelectProps = {
  id: string;
  options: MultiSelectOption[];
  value: string[];
  onChange: (value: string[]) => void;
  /** Shown when nothing is selected (empty selection = any value in rule scopes). */
  emptyLabel: ReactNode;
  disabled?: boolean;
  'aria-label'?: string;
};

/** A popover of checkboxes over a fixed option list; the trigger summarises the selection. */
export function MultiSelect({
  id,
  options,
  value,
  onChange,
  emptyLabel,
  disabled,
  ...props
}: MultiSelectProps) {
  const { t } = useT();
  const selected = new Set(value);
  const toggle = (v: string, on: boolean) => {
    // Keep the option order so that saved documents are stable.
    onChange(options.map((o) => o.value).filter((o) => (o === v ? on : selected.has(o))));
  };
  const labels = options.filter((o) => selected.has(o.value));
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          className="h-auto min-h-9 w-full justify-between font-normal whitespace-normal"
          disabled={disabled}
          aria-label={props['aria-label']}
        >
          <span className="text-left">
            {labels.length === 0 ? (
              <span className="text-muted-foreground">{emptyLabel}</span>
            ) : labels.length <= 3 ? (
              labels.map((o, i) => (
                <span key={o.value}>
                  {i > 0 ? ', ' : ''}
                  {o.label}
                </span>
              ))
            ) : (
              t('pricing:common.selectedCount', { count: labels.length })
            )}
          </span>
          <ChevronsUpDownIcon className="opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="max-h-80 w-72 overflow-y-auto p-2" align="start">
        <div className="flex flex-col gap-1">
          {options.length === 0 ? (
            <p className="text-muted-foreground p-2 text-sm">{t('pricing:common.noOptions')}</p>
          ) : (
            options.map((o) => {
              const cid = `${id}-${o.value}`;
              return (
                <div key={o.value} className="flex items-center gap-2 rounded-sm p-1">
                  <Checkbox
                    id={cid}
                    checked={selected.has(o.value)}
                    onCheckedChange={(c) => toggle(o.value, c === true)}
                  />
                  <Label htmlFor={cid} className="font-normal">
                    {o.label}
                  </Label>
                </div>
              );
            })
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
