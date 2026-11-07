import { useState, type ComponentProps } from 'react';

import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@porto/shared/ui/input-group';

import { parsePercentToBp } from '../../lib/format';

export type PercentInputProps = Omit<ComponentProps<'input'>, 'value' | 'onChange' | 'type'> & {
  /** Integer basis points (2200 = 22 %), or null when empty or not a valid percentage. */
  value: number | null;
  onChange: (bp: number | null) => void;
  /** Locale used to show the value (decimal comma in it). */
  locale?: string;
};

function bpToText(bp: number | null, locale: string): string {
  if (bp === null || !Number.isSafeInteger(bp)) return '';
  const negative = bp < 0;
  const abs = Math.abs(bp);
  const whole = Math.trunc(abs / 100);
  const frac = String(abs - whole * 100)
    .padStart(2, '0')
    .replace(/0+$/, '');
  const decimal = locale.startsWith('en') ? '.' : ',';
  return `${negative ? '-' : ''}${whole}${frac ? decimal + frac : ''}`;
}

/**
 * A percentage typed by the user (up to two decimals, comma or dot) kept as integer basis points in state. The
 * text stays as typed until blur; an invalid text gives null.
 */
export function PercentInput({
  value,
  onChange,
  locale = 'it',
  onBlur,
  ...props
}: PercentInputProps) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? bpToText(value, locale);
  return (
    <InputGroup>
      <InputGroupInput
        {...props}
        inputMode="decimal"
        value={shown}
        onChange={(e) => {
          setText(e.target.value);
          onChange(e.target.value.trim() === '' ? null : parsePercentToBp(e.target.value));
        }}
        onBlur={(e) => {
          if (text !== null && parsePercentToBp(text) !== null) setText(null);
          onBlur?.(e);
        }}
      />
      <InputGroupAddon align="inline-end">
        <InputGroupText>%</InputGroupText>
      </InputGroupAddon>
    </InputGroup>
  );
}
