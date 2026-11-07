import * as React from 'react';

import { Input } from '../ui/input';
import { centsToInput, parseCents } from './parse';

export type MoneyInputProps = Omit<React.ComponentProps<'input'>, 'value' | 'onChange' | 'type'> & {
  /** The amount in cents (null = empty). */
  value: number | null;
  /** Called with the parsed cents, or null while the text is empty or not a valid amount. */
  onValueChange: (cents: number | null) => void;
  locale?: string;
};

/**
 * Text input for an amount in euros that reports integer cents (m5-plan §9.2). Parsing follows Go
 * money.ParseCents (shared vectors in testdata/money/parse.json); invalid text sets aria-invalid.
 */
export function MoneyInput({
  value,
  onValueChange,
  locale = 'it',
  onBlur,
  ...props
}: MoneyInputProps) {
  const [text, setText] = React.useState(() => (value === null ? '' : centsToInput(value, locale)));
  // last is the amount the text stands for. After an external change, and after each edit (edits counts them), a
  // value that differs from it wins: a parent that clamped or refused what was just typed resets the text even
  // when it keeps the number it already held (its prop then does not change).
  const last = React.useRef(value);
  const [edits, setEdits] = React.useState(0);
  React.useEffect(() => {
    if (value !== last.current) {
      last.current = value;
      setText(value === null ? '' : centsToInput(value, locale));
    }
  }, [value, locale, edits]);
  const parsed = text.trim() === '' ? null : parseCents(text);
  const invalid = text.trim() !== '' && parsed === null;
  return (
    <Input
      {...props}
      type="text"
      inputMode="decimal"
      autoComplete="off"
      value={text}
      aria-invalid={invalid || props['aria-invalid'] ? true : undefined}
      onChange={(e) => {
        setText(e.target.value);
        setEdits((n) => n + 1);
        const next = e.target.value.trim() === '' ? null : parseCents(e.target.value);
        last.current = next;
        onValueChange(next);
      }}
      onBlur={(e) => {
        if (parsed !== null) setText(centsToInput(parsed, locale));
        onBlur?.(e);
      }}
    />
  );
}
