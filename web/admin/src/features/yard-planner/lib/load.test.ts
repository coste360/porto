import { heatClass, loadLabel } from './load';

describe('load heatmap', () => {
  it('maps load to theme token classes', () => {
    expect(heatClass(null, false)).toBe('bg-muted');
    expect(heatClass(null, false, 4000)).toContain('bg-chart-4');
    expect(heatClass(null, true, 4000)).toContain('bg-destructive');
    expect(heatClass(0, false)).toBe('bg-muted');
    expect(heatClass(3000, false)).toContain('bg-chart-3');
    expect(heatClass(7500, false)).toContain('bg-chart-2');
    expect(heatClass(10000, false)).toContain('bg-chart-5');
    expect(heatClass(11250, true)).toContain('bg-destructive');
    expect(loadLabel(7599)).toBe('75 %');
    expect(loadLabel(null)).toBe('—');
  });
});
