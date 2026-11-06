import { CheckIcon, ChevronsUpDownIcon } from 'lucide-react';
import { useState, type ReactNode } from 'react';

import { Button } from '@porto/shared/ui/button';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@porto/shared/ui/command';
import { Popover, PopoverContent, PopoverTrigger } from '@porto/shared/ui/popover';
import { cn } from '@porto/shared/lib/utils';

export type ComboboxOption = {
  value: string;
  label: string;
  /** Extra words matched by the search (codes, registration, ...). */
  keywords?: string[];
};

export type ElementComboboxProps = {
  options: readonly ComboboxOption[];
  value: string | null;
  onChange: (value: string | null) => void;
  placeholder: ReactNode;
  searchPlaceholder: string;
  emptyText: ReactNode;
  id?: string;
  disabled?: boolean;
  'aria-invalid'?: boolean;
  'aria-label'?: string;
  /** Called with the search text (for server-side search); options are filtered locally otherwise. */
  onSearch?: (text: string) => void;
};

/** Searchable single choice (popover + command): structures, customers, vessels, groups. */
export function ElementCombobox({
  options,
  value,
  onChange,
  placeholder,
  searchPlaceholder,
  emptyText,
  onSearch,
  ...props
}: ElementComboboxProps) {
  const [open, setOpen] = useState(false);
  const current = options.find((o) => o.value === value);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between"
          {...props}
        >
          {current ? current.label : <span className="text-muted-foreground">{placeholder}</span>}
          <ChevronsUpDownIcon className="opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-full p-0" align="start">
        <Command shouldFilter={!onSearch}>
          <CommandInput placeholder={searchPlaceholder} onValueChange={onSearch} />
          <CommandList>
            <CommandEmpty>{emptyText}</CommandEmpty>
            <CommandGroup>
              {options.map((o) => (
                <CommandItem
                  key={o.value}
                  value={o.value}
                  keywords={[o.label, ...(o.keywords ?? [])]}
                  onSelect={() => {
                    onChange(o.value === value ? null : o.value);
                    setOpen(false);
                  }}
                >
                  <CheckIcon className={cn(o.value === value ? 'opacity-100' : 'opacity-0')} />
                  {o.label}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
