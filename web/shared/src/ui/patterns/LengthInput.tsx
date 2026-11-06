import { useState, type ComponentProps } from 'react';

import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@porto/shared/ui/input-group';

import { parseMetresToCm } from '../../lib/format';

export type LengthInputProps = Omit<ComponentProps<'input'>, 'value' | 'onChange' | 'type'> & {
  /** Integer centimetres, or null when empty or not a valid length. */
  value: number | null;
  onChange: (cm: number | null) => void;
  /** Unit shown after the field (e.g. the translated "m"). */
  unit?: string;
  /** Locale used to show the value (decimal comma in it). */
  locale?: string;
};

function cmToText(cm: number | null, locale: string): string {
  if (cm === null || !Number.isSafeInteger(cm)) return '';
  const negative = cm < 0;
  const abs = Math.abs(cm);
  const whole = Math.trunc(abs / 100);
  const frac = String(abs - whole * 100).padStart(2, '0');
  const decimal = locale.startsWith('en') ? '.' : ',';
  return `${negative ? '-' : ''}${whole}${decimal}${frac}`;
}

/**
 * A length typed in metres (two decimals, comma or dot) kept as integer centimetres in state. The text the
 * user types stays as typed until blur; an invalid text gives null.
 */
export function LengthInput({
  value,
  onChange,
  unit = 'm',
  locale = 'it',
  onBlur,
  ...props
}: LengthInputProps) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? cmToText(value, locale);
  return (
    <InputGroup>
      <InputGroupInput
        {...props}
        inputMode="decimal"
        value={shown}
        onChange={(e) => {
          setText(e.target.value);
          onChange(e.target.value.trim() === '' ? null : parseMetresToCm(e.target.value));
        }}
        onBlur={(e) => {
          if (text !== null && parseMetresToCm(text) !== null) setText(null);
          onBlur?.(e);
        }}
      />
      <InputGroupAddon align="inline-end">
        <InputGroupText>{unit}</InputGroupText>
      </InputGroupAddon>
    </InputGroup>
  );
}
