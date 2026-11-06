import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { fromIsoDate, toIsoDate } from './DatePicker';
import { LengthInput } from './LengthInput';

describe('LengthInput', () => {
  it('shows centimetres as metres and reports integer centimetres', () => {
    const onChange = vi.fn();
    render(<LengthInput aria-label="length" value={1250} onChange={onChange} />);
    const input = screen.getByLabelText('length') as HTMLInputElement;
    expect(input.value).toBe('12,50');
    fireEvent.change(input, { target: { value: '7,3' } });
    expect(onChange).toHaveBeenLastCalledWith(730);
    fireEvent.change(input, { target: { value: '7,333' } });
    expect(onChange).toHaveBeenLastCalledWith(null);
    fireEvent.change(input, { target: { value: '' } });
    expect(onChange).toHaveBeenLastCalledWith(null);
  });
});

describe('DatePicker dates', () => {
  it('round-trips calendar days without timezone shifts', () => {
    const d = fromIsoDate('2026-03-29');
    expect(d && toIsoDate(d)).toBe('2026-03-29');
    expect(fromIsoDate('nope')).toBeUndefined();
  });
});
