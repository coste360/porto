import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { MoneyInput } from './MoneyInput';
import { PercentInput } from './PercentInput';

describe('MoneyInput', () => {
  it('shows cents as euros and reports integer cents', () => {
    const onChange = vi.fn();
    render(<MoneyInput aria-label="amount" value={25650} onChange={onChange} />);
    const input = screen.getByLabelText('amount') as HTMLInputElement;
    expect(input.value).toBe('256,50');
    fireEvent.change(input, { target: { value: '7,5' } });
    expect(onChange).toHaveBeenLastCalledWith(750);
    fireEvent.change(input, { target: { value: '1.234,56' } });
    expect(onChange).toHaveBeenLastCalledWith(123456);
    fireEvent.change(input, { target: { value: '7,555' } });
    expect(onChange).toHaveBeenLastCalledWith(null);
    fireEvent.change(input, { target: { value: '' } });
    expect(onChange).toHaveBeenLastCalledWith(null);
  });

  it('uses the English decimal point', () => {
    render(<MoneyInput aria-label="amount" value={-300} onChange={() => undefined} locale="en" />);
    expect((screen.getByLabelText('amount') as HTMLInputElement).value).toBe('-3.00');
  });
});

describe('PercentInput', () => {
  it('shows basis points as a percentage and reports integer basis points', () => {
    const onChange = vi.fn();
    render(<PercentInput aria-label="vat" value={2200} onChange={onChange} />);
    const input = screen.getByLabelText('vat') as HTMLInputElement;
    expect(input.value).toBe('22');
    fireEvent.change(input, { target: { value: '12,5' } });
    expect(onChange).toHaveBeenLastCalledWith(1250);
    fireEvent.change(input, { target: { value: '-10' } });
    expect(onChange).toHaveBeenLastCalledWith(-1000);
    fireEvent.change(input, { target: { value: '1,234' } });
    expect(onChange).toHaveBeenLastCalledWith(null);
  });

  it('keeps two decimals at most when showing', () => {
    render(<PercentInput aria-label="p" value={1205} onChange={() => undefined} />);
    expect((screen.getByLabelText('p') as HTMLInputElement).value).toBe('12,05');
  });
});
