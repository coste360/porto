import { maintenanceLegend, maintenanceStyle, maintenanceStyles } from './maintenance';

const e = (id: string, open: number, overdue = 0, oos = false) => ({
  element_type: 'berth' as const,
  element_id: id,
  open,
  overdue,
  out_of_service: oos,
});

describe('maintenance lens', () => {
  it('ranks out of service over overdue over open, never by colour only', () => {
    expect(maintenanceStyle(e('a', 2, 1, true))).toEqual({
      key: 'out_of_service',
      colorToken: 'maintenance',
      glyph: 'lock',
      badge: '2',
    });
    expect(maintenanceStyle(e('b', 1, 1))).toMatchObject({ key: 'overdue', glyph: 'alert' });
    expect(maintenanceStyle(e('c', 3))).toMatchObject({ key: 'open', glyph: 'wrench', badge: '3' });
    expect(maintenanceStyle(e('d', 0))).toEqual({ key: 'none', colorToken: 'free' });
    expect(maintenanceStyle(undefined).key).toBe('none');
  });

  it('keys the styles by element and counts the legend', () => {
    const list = [e('a', 1, 0, true), e('b', 1, 1), e('c', 1)];
    expect(maintenanceStyles(list).get('berth:b')?.key).toBe('overdue');
    expect(maintenanceLegend(list)).toEqual({ out_of_service: 1, overdue: 1, open: 1, none: 0 });
  });
});
