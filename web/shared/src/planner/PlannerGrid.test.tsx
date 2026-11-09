import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { buildAxis } from './model';
import { PlannerGrid, type PlannerGridSegment } from './PlannerGrid';

const axis = buildAxis({
  view: 'hours',
  from: '2027-06-12',
  days: 1,
  openFrom: 16,
  openTo: 38,
  bands: [],
  cellWidth: 20,
});
const rows = [
  { id: 'u1', label: 'GOM1' },
  { id: 'u2', label: 'GOM2' },
  { id: 's1', label: 'Marco', readOnly: true, group: 'skippers' },
];
const segments: PlannerGridSegment[] = [
  {
    key: 'a',
    rowId: 'u1',
    bookingId: 'b1',
    kind: 'booked',
    status: 'confirmed',
    range: { start: { date: '2027-06-12', slot: 20 }, end: { date: '2027-06-12', slot: 24 } },
    label: 'NL1',
    ariaLabel: 'NL1 confermato 10:00-12:00',
  },
  {
    key: 'b',
    rowId: 'u1',
    blockId: 'k1',
    kind: 'block',
    range: { start: { date: '2027-06-12', slot: 30 }, end: { date: '2027-06-12', slot: 32 } },
    label: '',
    ariaLabel: 'Fermo',
  },
];

function renderGrid(extra: Partial<Parameters<typeof PlannerGrid>[0]> = {}) {
  const onCreate = vi.fn();
  const onMove = vi.fn();
  const onSegment = vi.fn();
  render(
    <PlannerGrid
      axis={axis}
      rows={rows}
      segments={segments}
      label="Planner"
      cellLabel={(r, i) => `${r.label} ${i}`}
      onCreate={onCreate}
      onMove={onMove}
      onSegment={onSegment}
      {...extra}
    />,
  );
  return { onCreate, onMove, onSegment };
}

describe('PlannerGrid', () => {
  it('renders rows, cells and named segments', () => {
    renderGrid({ highlight: ['b1'] });
    expect(screen.getByRole('application', { name: 'Planner' })).toBeInTheDocument();
    expect(screen.getAllByTestId(/^planner-cell-/)).toHaveLength(3 * 30);
    expect(screen.getByRole('group', { name: 'GOM2' })).toBeInTheDocument();
    const booking = screen.getByRole('button', { name: 'NL1 confermato 10:00-12:00' });
    expect(booking.querySelector('rect')?.getAttribute('class')).toContain('fill-chart-1');
    expect(booking.querySelector('rect')?.getAttribute('class')).toContain('stroke-destructive');
    expect(screen.getByRole('img', { name: 'Fermo' })).toBeInTheDocument();
  });

  it('creates by dragging over free cells and never on a read-only row', () => {
    const { onCreate } = renderGrid();
    fireEvent.pointerDown(screen.getByTestId('planner-cell-GOM2-8'), { button: 0 });
    fireEvent.pointerEnter(screen.getByTestId('planner-cell-GOM2-11'));
    expect(screen.getByTestId('planner-ghost')).toBeInTheDocument();
    fireEvent.pointerUp(screen.getByRole('application'));
    expect(onCreate).toHaveBeenCalledWith('u2', {
      start: { date: '2027-06-12', slot: 20 },
      end: { date: '2027-06-12', slot: 24 },
    });
    fireEvent.pointerDown(screen.getByTestId('planner-cell-Marco-8'), { button: 0 });
    fireEvent.pointerUp(screen.getByRole('application'));
    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it('moves a booking to another row and opens it on a plain press', () => {
    const { onMove, onSegment } = renderGrid();
    const booking = screen.getByRole('button', { name: /NL1/ });
    fireEvent.pointerDown(booking, { button: 0 });
    fireEvent.pointerUp(screen.getByRole('application'));
    expect(onSegment).toHaveBeenCalledTimes(1);
    fireEvent.pointerDown(booking, { button: 0 });
    // jsdom has no layout: the press anchors on the booking's first cell (8).
    fireEvent.pointerEnter(screen.getByTestId('planner-cell-GOM2-10'));
    fireEvent.pointerUp(screen.getByRole('application'));
    expect(onMove).toHaveBeenCalledWith('b1', 'u1', 'u2', {
      start: { date: '2027-06-12', slot: 22 },
      end: { date: '2027-06-12', slot: 26 },
    });
  });

  it('selects with the keyboard and creates with Enter', () => {
    const { onCreate, onSegment } = renderGrid();
    const grid = screen.getByRole('application');
    grid.focus();
    fireEvent.keyDown(grid, { key: 'ArrowRight' });
    fireEvent.keyDown(grid, { key: 'ArrowDown' });
    fireEvent.keyDown(grid, { key: 'ArrowRight', shiftKey: true });
    expect(screen.getByTestId('planner-selection')).toBeInTheDocument();
    fireEvent.keyDown(grid, { key: 'Enter' });
    expect(onCreate).toHaveBeenCalledWith('u2', {
      start: { date: '2027-06-12', slot: 12 },
      end: { date: '2027-06-12', slot: 14 },
    });
    fireEvent.keyDown(grid, { key: 'Escape' });
    expect(screen.queryByTestId('planner-selection')).toBeNull();
    fireEvent.keyDown(screen.getByRole('button', { name: /NL1/ }), { key: 'Enter' });
    expect(onSegment).toHaveBeenCalledTimes(1);
  });
});
