import { useState, type ComponentProps } from 'react';

import { Input } from '@porto/shared/ui/input';

export type IntegerInputProps = Omit<ComponentProps<'input'>, 'value' | 'onChange' | 'type'> & {
  /** The integer, or null when empty; an invalid text gives NaN so that the form can flag it. */
  value: number | null;
  onChange: (value: number | null) => void;
  allowNegative?: boolean;
};

/** An integer typed as text (no floats, no exponent): sort orders, priorities, nights, days. */
export function IntegerInput({
  value,
  onChange,
  allowNegative = false,
  onBlur,
  ...props
}: IntegerInputProps) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? (value === null || Number.isNaN(value) ? '' : String(value));
  const re = allowNegative ? /^-?\d{1,9}$/ : /^\d{1,9}$/;
  return (
    <Input
      {...props}
      inputMode="numeric"
      value={shown}
      onChange={(e) => {
        const s = e.target.value.trim();
        setText(e.target.value);
        onChange(s === '' ? null : re.test(s) ? Number(s) : Number.NaN);
      }}
      onBlur={(e) => {
        if (text !== null && re.test(text.trim())) setText(null);
        onBlur?.(e);
      }}
    />
  );
}
