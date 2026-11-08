import { addHours, durationLabel, elapsedMinutes, idleTimer, timerReducer } from './timer';

const titleOf = () => 'Carteggiatura';

describe('yard phone timer', () => {
  it('runs a start queued offline until the server knows it, then follows the server', () => {
    let s = timerReducer(idleTimer, {
      type: 'started',
      taskId: 't1',
      title: 'Carteggiatura',
      startedAt: '2027-06-01T06:00:00Z',
      startOp: { opId: 'op1', lane: 'timer:x' },
    });
    expect(s.status).toBe('running');
    // offline: the cached work list has no running timer, the local start stays
    s = timerReducer(s, { type: 'server', running: null, titleOf });
    expect(s).toMatchObject({ status: 'running', taskId: 't1' });
    // online: the server's entry wins and gives the id for stop
    s = timerReducer(s, {
      type: 'server',
      running: { id: 'ts1', task_id: 't1', started_at: '2027-06-01T06:00:00Z' },
      titleOf,
    });
    expect(s).toMatchObject({ status: 'running', timesheetId: 'ts1' });
    // stopped elsewhere: idle
    expect(timerReducer(s, { type: 'server', running: null, titleOf })).toEqual(idleTimer);
    expect(timerReducer(s, { type: 'stopped' })).toEqual(idleTimer);
  });

  it('formats durations and clamps the hours keypad', () => {
    expect(elapsedMinutes('2027-06-01T06:00:00Z', new Date('2027-06-01T07:05:30Z'))).toBe(65);
    expect(elapsedMinutes('bad', new Date())).toBe(0);
    expect(durationLabel(65)).toBe('1 h 05');
    expect(addHours(0, 500)).toBe(500);
    expect(addHours(15750, 1000)).toBe(16000);
    expect(addHours(500, -1000)).toBe(0);
  });
});
