import { useState, type ComponentProps } from 'react';

import { formatQtyMilli, parseQtyToMilli } from '@porto/shared/lib/format';
import { Input } from '@porto/shared/ui/input';

export type QtyInputProps = Omit<ComponentProps<'input'>, 'value' | 'onChange' | 'type'> & {
  /** Integer thousandths (1500 = 1,5), or null when empty or invalid. */
  value: number | null;
  onChange: (milli: number | null) => void;
  locale?: string;
};

/** A quantity typed with up to three decimals, kept as integer thousandths (qty.Milli). */
export function QtyInput({ value, onChange, locale = 'it', onBlur, ...props }: QtyInputProps) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? (value === null ? '' : formatQtyMilli(value, locale));
  return (
    <Input
      {...props}
      inputMode="decimal"
      value={shown}
      onChange={(e) => {
        setText(e.target.value);
        onChange(e.target.value.trim() === '' ? null : parseQtyToMilli(e.target.value, locale));
      }}
      onBlur={(e) => {
        if (text !== null && parseQtyToMilli(text, locale) !== null) setText(null);
        onBlur?.(e);
      }}
    />
  );
}
