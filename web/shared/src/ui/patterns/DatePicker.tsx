import { CalendarIcon } from 'lucide-react';
import { useState } from 'react';

import { Button } from '@porto/shared/ui/button';
import { Calendar } from '@porto/shared/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '@porto/shared/ui/popover';

import { formatIsoDate } from '../../lib/format';

export type DatePickerProps = {
  /** Calendar date `YYYY-MM-DD`, or null. */
  value: string | null;
  onChange: (date: string | null) => void;
  id?: string;
  placeholder?: string;
  disabled?: boolean;
  'aria-invalid'?: boolean;
  'aria-label'?: string;
};

const pad = (n: number) => String(n).padStart(2, '0');

/** Local calendar day of a Date picked in the calendar → `YYYY-MM-DD` (no timezone conversion). */
export function toIsoDate(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** `YYYY-MM-DD` → a local Date at midnight for the calendar, or undefined. */
export function fromIsoDate(s: string | null): Date | undefined {
  const m = s ? /^(\d{4})-(\d{2})-(\d{2})$/.exec(s) : null;
  return m ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])) : undefined;
}

/** A calendar date picker (popover + calendar) whose value is a `YYYY-MM-DD` string. */
export function DatePicker({ value, onChange, placeholder, ...props }: DatePickerProps) {
  const [open, setOpen] = useState(false);
  const selected = fromIsoDate(value);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" className="w-full justify-start font-normal" {...props}>
          <CalendarIcon />
          {value ? (
            formatIsoDate(value)
          ) : (
            <span className="text-muted-foreground">{placeholder}</span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="start">
        <Calendar
          mode="single"
          selected={selected}
          defaultMonth={selected}
          onSelect={(d) => {
            onChange(d ? toIsoDate(d) : null);
            setOpen(false);
          }}
        />
      </PopoverContent>
    </Popover>
  );
}
