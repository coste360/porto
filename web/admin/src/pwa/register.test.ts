import { toast } from 'sonner';

import { applyUpdate, registerServiceWorker, watchUpdates } from './register';

vi.mock('sonner', () => ({ toast: vi.fn() }));

type Listener = () => void;

function fakeRegistration(waiting: { postMessage: (m: unknown) => void } | null) {
  const listeners: Record<string, Listener[]> = {};
  return {
    waiting,
    installing: null as null | {
      state: string;
      addEventListener: (t: string, l: Listener) => void;
    },
    addEventListener: (t: string, l: Listener) => (listeners[t] ??= []).push(l),
    fire: (t: string) => listeners[t]?.forEach((l) => l()),
  };
}

function stubServiceWorker(controller: unknown) {
  const listeners: Record<string, Listener[]> = {};
  Object.defineProperty(navigator, 'serviceWorker', {
    configurable: true,
    value: {
      controller,
      register: vi.fn(),
      addEventListener: (t: string, l: Listener) => (listeners[t] ??= []).push(l),
    },
  });
  return listeners;
}

describe('service worker registration', () => {
  beforeEach(() => {
    vi.mocked(toast).mockClear();
    delete document.documentElement.dataset.portoSwUpdate;
  });

  it('announces a waiting worker when another one controls the page, and Aggiorna hands over', () => {
    const sw = stubServiceWorker({});
    const waiting = { postMessage: vi.fn() };
    const reg = fakeRegistration(waiting);
    watchUpdates(reg as unknown as ServiceWorkerRegistration);
    expect(document.documentElement.dataset.portoSwUpdate).toBe('waiting');
    const [message, opts] = vi.mocked(toast).mock.calls[0]!;
    // The browser locale picks the language (jsdom: English).
    expect(['Nuova versione disponibile', 'New version available']).toContain(message);
    const action = (opts as { action: { label: string; onClick: () => void } }).action;
    expect(['Aggiorna', 'Update']).toContain(action.label);
    applyUpdate(reg as unknown as ServiceWorkerRegistration);
    expect(waiting.postMessage).toHaveBeenCalledWith({ type: 'SKIP_WAITING' });
    expect(sw.controllerchange).toHaveLength(1);
  });

  it('does not announce the first install (nothing controls the page yet)', () => {
    stubServiceWorker(null);
    const reg = fakeRegistration({ postMessage: vi.fn() });
    watchUpdates(reg as unknown as ServiceWorkerRegistration);
    expect(toast).not.toHaveBeenCalled();
  });

  it('announces a worker installed while the page runs', () => {
    stubServiceWorker({});
    const reg = fakeRegistration(null);
    watchUpdates(reg as unknown as ServiceWorkerRegistration);
    const changes: Listener[] = [];
    reg.installing = { state: 'installing', addEventListener: (_t, l) => changes.push(l) };
    reg.fire('updatefound');
    reg.installing.state = 'installed';
    changes.forEach((l) => l());
    expect(toast).toHaveBeenCalledTimes(1);
  });

  it('is skipped in development and inside the desktop window', async () => {
    stubServiceWorker(null);
    expect(await registerServiceWorker()).toBe('skipped'); // Vitest runs in dev mode
    expect(document.documentElement.dataset.portoSw).toBe('skipped');
  });
});
