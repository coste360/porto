import { useState, type ComponentProps } from 'react';

import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@porto/shared/ui/input-group';

import { parseEuroToCents } from '../../lib/format';

export type MoneyInputProps = Omit<ComponentProps<'input'>, 'value' | 'onChange' | 'type'> & {
  /** Integer cents, or null when empty or not a valid amount. */
  value: number | null;
  onChange: (cents: number | null) => void;
  /** Currency symbol shown after the field. */
  symbol?: string;
  /** Locale used to show the value (decimal comma in it). */
  locale?: string;
};

function centsToText(cents: number | null, locale: string): string {
  if (cents === null || !Number.isSafeInteger(cents)) return '';
  const negative = cents < 0;
  const abs = Math.abs(cents);
  const whole = Math.trunc(abs / 100);
  const frac = String(abs - whole * 100).padStart(2, '0');
  const decimal = locale.startsWith('en') ? '.' : ',';
  return `${negative ? '-' : ''}${whole}${decimal}${frac}`;
}

/**
 * An amount typed in euros (two decimals, comma or dot, optional thousands separators) kept as integer cents in
 * state. The text stays as typed until blur; an invalid text gives null.
 */
export function MoneyInput({
  value,
  onChange,
  symbol = '€',
  locale = 'it',
  onBlur,
  ...props
}: MoneyInputProps) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? centsToText(value, locale);
  return (
    <InputGroup>
      <InputGroupInput
        {...props}
        inputMode="decimal"
        value={shown}
        onChange={(e) => {
          setText(e.target.value);
          onChange(e.target.value.trim() === '' ? null : parseEuroToCents(e.target.value));
        }}
        onBlur={(e) => {
          if (text !== null && parseEuroToCents(text) !== null) setText(null);
          onBlur?.(e);
        }}
      />
      <InputGroupAddon align="inline-end">
        <InputGroupText>{symbol}</InputGroupText>
      </InputGroupAddon>
    </InputGroup>
  );
}
