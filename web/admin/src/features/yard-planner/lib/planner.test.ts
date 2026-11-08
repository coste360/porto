import type { PlannerRow, PlannerTask } from '@porto/shared/api';

import {
  datesOf,
  defaultHours,
  dropFeedback,
  existingHours,
  gridReducer,
  hoursLabel,
  initialGrid,
  parseHours,
  periodOf,
  shiftAnchor,
  trayTasks,
} from './planner';

const row: PlannerRow = {
  employee_id: 'e1',
  code: 'P1',
  display_name: 'Paolo Neri',
  department: '',
  qualification_ids: ['paint'],
  days: [
    { date: '2027-06-02', capacity_milli: 8000, assigned_milli: 6000, overload: false },
    { date: '2027-06-03', capacity_milli: 0, assigned_milli: 0, overload: false },
  ],
};

const task: PlannerTask = {
  task_id: 't1',
  work_order_id: 'o1',
  work_order_number: 'C2027-000001',
  title: 'Carteggiatura',
  qualification_id: 'paint',
  estimated_hours_milli: 10000,
  remaining_milli: 10000,
  assigned_milli: 4000,
  progress_bp: 0,
  depends_on: [],
  version: 1,
};

describe('planner periods', () => {
  it('computes day, week and month periods and shifts them', () => {
    expect(periodOf('day', '2027-06-02')).toEqual({ from: '2027-06-02', to: '2027-06-02' });
    expect(periodOf('week', '2027-06-02')).toEqual({ from: '2027-05-31', to: '2027-06-06' });
    expect(periodOf('month', '2028-02-10')).toEqual({ from: '2028-02-01', to: '2028-02-29' });
    expect(shiftAnchor('week', '2027-06-02', 1)).toBe('2027-06-09');
    expect(shiftAnchor('month', '2027-12-15', 1)).toBe('2028-01-01');
    expect(shiftAnchor('day', '2027-06-01', -1)).toBe('2027-05-31');
    expect(datesOf('2027-05-30', '2027-06-02')).toEqual([
      '2027-05-30',
      '2027-05-31',
      '2027-06-01',
      '2027-06-02',
    ]);
  });

  it('formats and parses hours without floats', () => {
    expect(hoursLabel(7500)).toBe('7,5');
    expect(hoursLabel(8000)).toBe('8');
    expect(hoursLabel(1667)).toBe('1,667');
    expect(parseHours('7,5')).toBe(7500);
    expect(parseHours('0.25')).toBe(250);
    expect(parseHours('17')).toBeNull();
    expect(parseHours('abc')).toBeNull();
    expect(parseHours('0')).toBeNull();
  });
});

describe('drop feedback', () => {
  it('flags qualification mismatch and over capacity', () => {
    expect(dropFeedback(row, '2027-06-02', task, 2000)).toEqual({
      qualificationMismatch: false,
      overCapacity: false,
      capacity: 8000,
      assignedAfter: 8000,
    });
    expect(dropFeedback(row, '2027-06-02', task, 2500).overCapacity).toBe(true);
    expect(
      dropFeedback(row, '2027-06-02', { ...task, qualification_id: 'mech' }, 1000)
        .qualificationMismatch,
    ).toBe(true);
    expect(dropFeedback(row, '2027-06-03', task, 1000).overCapacity).toBe(true);
  });

  it('proposes the hours left capped by the free capacity', () => {
    expect(defaultHours(row, '2027-06-02', task)).toBe(2000);
    expect(defaultHours(row, '2027-06-03', { ...task, assigned_milli: 9100 })).toBe(750);
    expect(
      existingHours(
        [
          {
            id: 'a',
            task_id: 't1',
            work_order_id: 'o1',
            employee_id: 'e1',
            work_date: '2027-06-02',
            hours_milli: 3000,
            forced_qualification: false,
            overload: false,
            version: 1,
          },
        ],
        't1',
        'e1',
        '2027-06-02',
      ),
    ).toBe(3000);
    expect(
      trayTasks([task, { ...task, task_id: 't2', assigned_milli: 10000 }]).map((t) => t.task_id),
    ).toEqual(['t1']);
  });
});

describe('grid reducer', () => {
  it('moves the keyboard cursor within bounds and confirms with Enter', () => {
    let s = gridReducer(initialGrid, { type: 'pick', taskId: 't1' });
    expect(s.cursor).toEqual({ row: 0, col: 0 });
    s = gridReducer(s, { type: 'key', key: 'ArrowLeft', rows: 2, cols: 7 });
    expect(s.cursor).toEqual({ row: 0, col: 0 });
    s = gridReducer(s, { type: 'key', key: 'ArrowRight', rows: 2, cols: 7 });
    s = gridReducer(s, { type: 'key', key: 'ArrowDown', rows: 2, cols: 7 });
    s = gridReducer(s, { type: 'key', key: 'ArrowDown', rows: 2, cols: 7 });
    expect(s.cursor).toEqual({ row: 1, col: 1 });
    s = gridReducer(s, { type: 'key', key: 'Enter', rows: 2, cols: 7 });
    expect(s.target).toEqual({ row: 1, col: 1 });
    expect(gridReducer(s, { type: 'key', key: 'Escape', rows: 2, cols: 7 })).toEqual(initialGrid);
  });

  it('drops a dragged task on the hovered cell, cancels outside the grid', () => {
    let s = gridReducer(initialGrid, { type: 'dragStart', taskId: 't1' });
    s = gridReducer(s, { type: 'over', cell: { row: 0, col: 3 } });
    s = gridReducer(s, { type: 'drop' });
    expect(s).toMatchObject({ taskId: 't1', dragging: false, target: { row: 0, col: 3 } });
    let c = gridReducer(initialGrid, { type: 'dragStart', taskId: 't1' });
    c = gridReducer(c, { type: 'over', cell: null });
    expect(gridReducer(c, { type: 'drop' })).toEqual(initialGrid);
    expect(gridReducer(initialGrid, { type: 'key', key: 'Enter', rows: 1, cols: 1 })).toEqual(
      initialGrid,
    );
  });
});
