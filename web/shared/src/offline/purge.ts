// Logout and revoke (m6b-plan §4.3, INV-84): logout deletes the whole database (cache, ops, photos); the service
// worker's shell cache holds no user data and is kept.
import { deleteDb } from './db';
import { resetQueueMemory } from './queue';

/** Deletes every offline record of this origin (logout). */
export async function purgeOfflineData(): Promise<void> {
  await deleteDb();
  resetQueueMemory();
}
