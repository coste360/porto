// embed.js (public/, m8-plan §10) in jsdom: iframe creation, resize, CustomEvent and the origin check.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { afterEach, beforeEach, describe, expect, it } from 'vitest';

const SOURCE = readFileSync(join(__dirname, '..', 'public', 'embed.js'), 'utf8');
const PORTO = 'https://porto.example';

function load() {
  const s = document.createElement('script');
  s.src = `${PORTO}/embed.js`;
  document.head.appendChild(s);
  // Run the loader as the browser would, with the script element in the document.
  new Function(SOURCE)();
}

function post(frame: HTMLIFrameElement, data: unknown, origin = PORTO) {
  window.dispatchEvent(new MessageEvent('message', { data, origin, source: frame.contentWindow }));
}

describe('embed.js', () => {
  beforeEach(() => {
    document.body.innerHTML =
      '<div id="w" data-porto="porto-a" data-lang="en"></div><div data-porto="BAD SLUG"></div>';
    delete (window as unknown as { PortoBooking?: unknown }).PortoBooking;
  });
  afterEach(() => {
    document.head.querySelectorAll('script').forEach((s) => s.remove());
  });

  it('creates the iframe with embed, parent and lang', () => {
    load();
    const frames = document.querySelectorAll('iframe');
    expect(frames).toHaveLength(1);
    const url = new URL(frames[0]!.src);
    expect(url.origin + url.pathname).toBe(`${PORTO}/b/porto-a`);
    expect(url.searchParams.get('embed')).toBe('1');
    expect(url.searchParams.get('parent')).toBe(window.location.origin);
    expect(url.searchParams.get('lang')).toBe('en');
    expect(frames[0]!.getAttribute('allow')).toBe('payment');
    expect(frames[0]!.title).not.toBe('');
  });

  it('resizes on porto:resize and re-dispatches step and booked from the Porto origin only', () => {
    load();
    const frame = document.querySelector('iframe')!;
    const container = document.getElementById('w')!;
    const events: CustomEvent[] = [];
    container.addEventListener('porto:booking', (e) => events.push(e as CustomEvent));

    post(frame, { type: 'porto:resize', height: 912 });
    expect(frame.height).toBe('912');
    post(frame, { type: 'porto:resize', height: 5 });
    expect(frame.height).toBe('912');

    post(frame, { type: 'porto:step', step: 'details' });
    post(frame, { type: 'porto:booked', reference: 'K7M2Q9XA' });
    expect(events.map((e) => e.detail)).toEqual([
      { type: 'step', step: 'details' },
      { type: 'booked', reference: 'K7M2Q9XA' },
    ]);

    // Another origin, or a message not from the iframe, is ignored.
    post(frame, { type: 'porto:booked', reference: 'EVIL0000' }, 'https://evil.example');
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'porto:booked', reference: 'X' },
        origin: PORTO,
        source: window,
      }),
    );
    post(frame, 'porto:booked');
    expect(events).toHaveLength(2);
  });

  it('does not mount twice', () => {
    load();
    (window as unknown as { PortoBooking: { mount: () => void } }).PortoBooking.mount();
    expect(document.querySelectorAll('iframe')).toHaveLength(1);
  });
});
