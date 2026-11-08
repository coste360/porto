// Timer state of the yard phone (m10-plan §7, D24, D25): the running entry comes from the server when known
// (getMyYardWork.running_timer); a start queued offline is kept locally until the queue sends it, so that "Stop"
// can depend on it. Pure: no React, no storage access here.

export type TimerState =
  | { status: 'idle' }
  | {
      status: 'running';
      taskId: string;
      title: string;
      startedAt: string;
      /** Known once the server stored the start. */
      timesheetId?: string;
      /** The queued start op, while the server has not answered yet. */
      startOp?: { opId: string; lane: string };
    };

export type TimerAction =
  | {
      type: 'server';
      running: { id: string; task_id?: string; started_at?: string } | null | undefined;
      titleOf: (taskId: string) => string;
    }
  | {
      type: 'started';
      taskId: string;
      title: string;
      startedAt: string;
      startOp: { opId: string; lane: string };
    }
  | { type: 'stopped' };

export const idleTimer: TimerState = { status: 'idle' };

export function timerReducer(s: TimerState, a: TimerAction): TimerState {
  switch (a.type) {
    case 'started':
      return {
        status: 'running',
        taskId: a.taskId,
        title: a.title,
        startedAt: a.startedAt,
        startOp: a.startOp,
      };
    case 'stopped':
      return idleTimer;
    case 'server': {
      if (a.running && a.running.task_id) {
        // The server knows the running entry: it wins over a local queued start of the same task.
        if (s.status === 'running' && s.taskId !== a.running.task_id && s.startOp) return s;
        return {
          status: 'running',
          taskId: a.running.task_id,
          title: a.titleOf(a.running.task_id),
          startedAt: a.running.started_at ?? (s.status === 'running' ? s.startedAt : ''),
          timesheetId: a.running.id,
        };
      }
      // No running entry on the server: a start still queued locally keeps running; otherwise idle.
      return s.status === 'running' && s.startOp && !s.timesheetId ? s : idleTimer;
    }
  }
}

/** Elapsed minutes between two instants (>= 0). */
export function elapsedMinutes(startedAt: string, now: Date): number {
  const t = Date.parse(startedAt);
  if (Number.isNaN(t)) return 0;
  return Math.max(0, Math.floor((now.getTime() - t) / 60000));
}

/** "1 h 05" from minutes. */
export function durationLabel(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return `${h} h ${String(m).padStart(2, '0')}`;
}

/** Manual hours keypad: adds a step (milli) and clamps to 0.25..16 h in 0.25 h steps. */
export function addHours(current: number, step: number): number {
  const v = Math.min(Math.max(current + step, 0), 16000);
  return v - (v % 250);
}
