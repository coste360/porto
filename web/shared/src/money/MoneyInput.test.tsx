import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import * as React from 'react';
import { describe, expect, it } from 'vitest';

import { MoneyInput } from './MoneyInput';

/** A parent that never accepts more than max cents and keeps its value when a typed amount is above it. */
function Clamped({ max, initial }: { max: number; initial: number }) {
  const [v, setV] = React.useState<number | null>(initial);
  return (
    <MoneyInput
      aria-label="importo"
      value={v}
      onValueChange={(c) => setV(c === null ? null : Math.min(c, max))}
    />
  );
}

describe('MoneyInput', () => {
  it('shows the clamped amount even when the parent keeps the value it already held', () => {
    render(<Clamped max={30000} initial={30000} />);
    const input = screen.getByLabelText('importo');
    expect(input).toHaveValue('300,00');
    fireEvent.change(input, { target: { value: '500' } });
    expect(input).toHaveValue('300,00');
  });

  it('keeps what is typed when the parent accepts it', async () => {
    render(<Clamped max={1_000_000} initial={0} />);
    const input = screen.getByLabelText('importo');
    await userEvent.clear(input);
    await userEvent.type(input, '12,5');
    expect(input).toHaveValue('12,5');
  });
});
