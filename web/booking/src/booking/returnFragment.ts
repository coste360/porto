/** Polling gives up after this long and says that the email will follow. */
export const VERIFY_TIMEOUT_MS = 3 * 60_000;

export type ReturnFragment = {
  statusToken: string | null;
  returnTo: string | null;
  cancelled: boolean;
};

/** Reads #st=…&rt=…&cancelled=1 from the URL fragment. */
export function parseReturnFragment(hash: string): ReturnFragment {
  const q = new URLSearchParams(hash.replace(/^#/, ''));
  return {
    statusToken: q.get('st'),
    returnTo: q.get('rt'),
    cancelled: q.get('cancelled') === '1',
  };
}
