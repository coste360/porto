import { rmSync } from 'node:fs';

import { RUN_DIR } from './env';

/** Removes the run's scratch directory (database, mail); set PORTO_E2E_KEEP=1 to inspect it after a run. */
export default function globalTeardown() {
  if (process.env.PORTO_E2E_KEEP) {
    console.log(`porto e2e: data kept in ${RUN_DIR}`);
    return;
  }
  rmSync(RUN_DIR, { recursive: true, force: true });
}
