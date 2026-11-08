// Pure helpers of the yard planner grid (m10-plan §6, §14.3): periods, hours, drop feedback and the reducer of the
// pointer drag and its keyboard alternative. No React, no API.
import type { Assignment, PlannerRow, PlannerTask } from '@porto/shared/api';

export type PlannerView = 'day' | 'week' | 'month';

/** Adds days to a YYYY-MM-DD date (UTC arithmetic: dates are calendar labels). */
export function addDays(date: string, n: number): string {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}

/** ISO weekday of a date, 1 = Monday. */
export function isoWeekday(date: string): number {
  const wd = new Date(`${date}T00:00:00Z`).getUTCDay();
  return wd === 0 ? 7 : wd;
}

/** The period of a view around an anchor date: the day, its Monday-Sunday week or its month. */
export function periodOf(view: PlannerView, anchor: string): { from: string; to: string } {
  if (view === 'day') return { from: anchor, to: anchor };
  if (view === 'week') {
    const from = addDays(anchor, 1 - isoWeekday(anchor));
    return { from, to: addDays(from, 6) };
  }
  const [y, m] = anchor.split('-').map(Number) as [number, number];
  const from = `${y}-${String(m).padStart(2, '0')}-01`;
  const next = new Date(Date.UTC(y, m, 1));
  next.setUTCDate(0);
  return { from, to: next.toISOString().slice(0, 10) };
}

/** Moves the anchor one period back (-1) or forward (+1). */
export function shiftAnchor(view: PlannerView, anchor: string, dir: -1 | 1): string {
  if (view === 'day') return addDays(anchor, dir);
  if (view === 'week') return addDays(anchor, 7 * dir);
  const [y, m] = anchor.split('-').map(Number) as [number, number];
  const d = new Date(Date.UTC(y, m - 1 + dir, 1));
  return d.toISOString().slice(0, 10);
}

/** Every date of a period. */
export function datesOf(from: string, to: string): string[] {
  const out: string[] = [];
  for (let d = from; d <= to && out.length < 93; d = addDays(d, 1)) out.push(d);
  return out;
}

/** Hours milli as a short label with a comma ("7,5"). */
export function hoursLabel(milli: number): string {
  const whole = Math.trunc(milli / 1000);
  const frac = Math.abs(milli % 1000);
  if (frac === 0) return String(whole);
  return `${whole},${String(frac).padStart(3, '0').replace(/0+$/, '')}`;
}

/** Parses "7,5" or "7.5" hours into milli (null when invalid, at most 3 decimals, 0 < h <= 16). */
export function parseHours(text: string): number | null {
  const m = /^\s*(\d{1,2})(?:[.,](\d{1,3}))?\s*$/.exec(text);
  if (!m) return null;
  const milli = Number(m[1]) * 1000 + Number((m[2] ?? '').padEnd(3, '0') || '0');
  return milli > 0 && milli <= 16000 ? milli : null;
}

/** Hours of a task on an employee and day already assigned. */
export function existingHours(
  assignments: readonly Assignment[],
  taskId: string,
  employeeId: string,
  date: string,
): number {
  return assignments
    .filter((a) => a.task_id === taskId && a.employee_id === employeeId && a.work_date === date)
    .reduce((s, a) => s + a.hours_milli, 0);
}

export type DropFeedback = {
  qualificationMismatch: boolean;
  overCapacity: boolean;
  capacity: number;
  assignedAfter: number;
};

/** What happens when hours of a task are dropped on an employee's day (soft rules, confirmed by the user). */
export function dropFeedback(
  row: PlannerRow,
  date: string,
  task: PlannerTask,
  hours: number,
): DropFeedback {
  const day = row.days.find((d) => d.date === date);
  const capacity = day?.capacity_milli ?? 0;
  const assignedAfter = (day?.assigned_milli ?? 0) + hours;
  return {
    qualificationMismatch:
      !!task.qualification_id && !row.qualification_ids.includes(task.qualification_id),
    overCapacity: assignedAfter > capacity,
    capacity,
    assignedAfter,
  };
}

/** Default hours of a drop: what is left to plan of the task, capped by the free capacity of the day, in 0.25 h. */
export function defaultHours(row: PlannerRow, date: string, task: PlannerTask): number {
  const day = row.days.find((d) => d.date === date);
  const free = Math.max((day?.capacity_milli ?? 0) - (day?.assigned_milli ?? 0), 0);
  const left = Math.max(task.remaining_milli - task.assigned_milli, 0);
  const h = Math.min(left || 1000, free || 1000);
  return Math.max(250, h - (h % 250));
}

/** Tasks of the tray "Da pianificare": remaining hours not yet assigned from today on. */
export function trayTasks(tasks: readonly PlannerTask[]): PlannerTask[] {
  return tasks.filter((t) => t.remaining_milli > t.assigned_milli);
}

// ---- drag and keyboard reducer ----

export type Cell = { row: number; col: number };

export type GridState = {
  /** Task picked from the tray (keyboard or pointer). */
  taskId: string | null;
  /** Cell under the pointer or the keyboard cursor. */
  cursor: Cell | null;
  dragging: boolean;
  /** Cell confirmed by a drop or Enter: the page opens the hours dialog. */
  target: Cell | null;
};

export type GridAction =
  | { type: 'pick'; taskId: string }
  | { type: 'dragStart'; taskId: string }
  | { type: 'over'; cell: Cell | null }
  | { type: 'drop' }
  | { type: 'key'; key: string; rows: number; cols: number }
  | { type: 'clear' };

export const initialGrid: GridState = { taskId: null, cursor: null, dragging: false, target: null };

function clamp(v: number, max: number): number {
  return Math.min(Math.max(v, 0), Math.max(max - 1, 0));
}

export function gridReducer(s: GridState, a: GridAction): GridState {
  switch (a.type) {
    case 'pick':
      return {
        taskId: a.taskId,
        cursor: s.cursor ?? { row: 0, col: 0 },
        dragging: false,
        target: null,
      };
    case 'dragStart':
      return { taskId: a.taskId, cursor: null, dragging: true, target: null };
    case 'over':
      return s.dragging ? { ...s, cursor: a.cell } : s;
    case 'drop':
      if (!s.dragging) return s;
      return s.cursor && s.taskId
        ? { ...s, dragging: false, target: s.cursor }
        : { ...initialGrid };
    case 'key': {
      if (!s.taskId || s.dragging) return s;
      const c = s.cursor ?? { row: 0, col: 0 };
      switch (a.key) {
        case 'ArrowUp':
          return { ...s, cursor: { row: clamp(c.row - 1, a.rows), col: c.col } };
        case 'ArrowDown':
          return { ...s, cursor: { row: clamp(c.row + 1, a.rows), col: c.col } };
        case 'ArrowLeft':
          return { ...s, cursor: { row: c.row, col: clamp(c.col - 1, a.cols) } };
        case 'ArrowRight':
          return { ...s, cursor: { row: c.row, col: clamp(c.col + 1, a.cols) } };
        case 'Enter':
          return a.rows > 0 && a.cols > 0 ? { ...s, target: c } : s;
        case 'Escape':
          return { ...initialGrid };
        default:
          return s;
      }
    }
    case 'clear':
      return { ...initialGrid };
  }
}
