import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PortoDesktopBridge } from '../desktop/bridge';
import { isExternalUrl, openExternal, openFile, printPage, saveBlob, saveText } from './download';

function installBridge(): PortoDesktopBridge & {
  [K in 'saveFile' | 'openExternal' | 'print']: ReturnType<typeof vi.fn>;
} {
  const bridge = {
    version: '1.0.0',
    saveFile: vi.fn(async () => '/Users/test/Downloads/x'),
    openExternal: vi.fn(async () => undefined),
    print: vi.fn(async () => undefined),
  };
  window.portoDesktop = bridge;
  return bridge;
}

afterEach(() => {
  delete window.portoDesktop;
  vi.restoreAllMocks();
});

describe('download helpers', () => {
  it('use the bridge inside the desktop window', async () => {
    const bridge = installBridge();
    const createUrl = vi.spyOn(URL, 'createObjectURL');
    await saveBlob(new Blob(['ciao'], { type: 'text/plain' }), 'saluto.txt');
    expect(bridge.saveFile).toHaveBeenCalledWith('saluto.txt', btoa('ciao'));
    expect(createUrl).not.toHaveBeenCalled();

    await saveText('{"a":1}', 'a.json', 'application/json');
    expect(bridge.saveFile).toHaveBeenLastCalledWith('a.json', btoa('{"a":1}'));

    await printPage();
    expect(bridge.print).toHaveBeenCalledOnce();

    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    await openFile(new Blob(['%PDF']));
    expect(bridge.saveFile).toHaveBeenLastCalledWith('porto-document.pdf', btoa('%PDF'));
    await openFile(new Blob(['<p>'], { type: 'text/html;charset=utf-8' }));
    expect(bridge.saveFile).toHaveBeenLastCalledWith('porto-document.html', btoa('<p>'));
    expect(open).not.toHaveBeenCalled();

    await openExternal('https://example.test/porto');
    expect(bridge.openExternal).toHaveBeenCalledWith('https://example.test/porto');
  });

  it('propagate a refused save', async () => {
    const bridge = installBridge();
    bridge.saveFile.mockRejectedValueOnce(new Error('too large'));
    await expect(saveBlob(new Blob(['x']), 'x.bin')).rejects.toThrow('too large');
  });

  it('fall back to the browser without the bridge', async () => {
    const createUrl = vi.fn(() => 'blob:porto/1');
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createUrl });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() });
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => undefined);
    await saveBlob(new Blob(['x']), 'x.csv');
    expect(createUrl).toHaveBeenCalledOnce();
    expect(click).toHaveBeenCalledOnce();
    expect(document.querySelector('a[download]')).toBeNull();

    const print = vi.spyOn(window, 'print').mockImplementation(() => undefined);
    await printPage();
    expect(print).toHaveBeenCalledOnce();

    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    await openFile(new Blob(['%PDF'], { type: 'application/pdf' }));
    expect(open).toHaveBeenCalledWith('blob:porto/1', '_blank', 'noopener');
  });

  it('open only https and mailto links outside the app', async () => {
    expect(isExternalUrl('https://example.test')).toBe(true);
    expect(isExternalUrl('mailto:info@example.test')).toBe(true);
    expect(isExternalUrl('http://example.test')).toBe(false);
    expect(isExternalUrl('javascript:alert(1)')).toBe(false);
    expect(isExternalUrl('not a url')).toBe(false);
    const bridge = installBridge();
    await expect(openExternal('javascript:alert(1)')).rejects.toThrow();
    expect(bridge.openExternal).not.toHaveBeenCalled();
  });
});
