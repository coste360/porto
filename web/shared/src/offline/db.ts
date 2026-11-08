// IndexedDB "porto-offline" (m6b-plan §4.5): a thin promise wrapper over indexedDB, no dependency.
//
// Stores: meta (key/value: schema, userId, marinaId, appVersion), cache (one persisted query bundle per
// `userId:marinaId`), ops (queued operations, key `id`, indexes byUserMarina and byLaneCreated) and blobs (photo
// bytes of presence.photo ops, keyed by op id, stored as ArrayBuffer + type: Blob cloning is not reliable on every
// engine). Upgrades are additive; a database written by a newer schema is opened read-only.

export const DB_NAME = 'porto-offline';
export const DB_VERSION = 1;

export type StoreName = 'meta' | 'cache' | 'ops' | 'blobs';

export type StoredBlob = { id: string; type: string; bytes: ArrayBuffer };

/** Thrown when the database was written by a newer version of Porto (offline:schemaNewer). */
export class OfflineSchemaNewerError extends Error {
  constructor() {
    super('offline: database written by a newer version');
    this.name = 'OfflineSchemaNewerError';
  }
}

let dbPromise: Promise<IDBDatabase> | null = null;
let readOnly = false;

function idb(): IDBFactory | undefined {
  return typeof indexedDB === 'undefined' ? undefined : indexedDB;
}

/** True when IndexedDB is usable in this context (not in some private modes or very old engines). */
export function offlineStorageAvailable(): boolean {
  return idb() !== undefined;
}

/** True when the database is newer than this build: nothing can be written. */
export function isReadOnly(): boolean {
  return readOnly;
}

function upgrade(db: IDBDatabase, oldVersion: number) {
  if (oldVersion < 1) {
    db.createObjectStore('meta');
    db.createObjectStore('cache');
    const ops = db.createObjectStore('ops', { keyPath: 'id' });
    ops.createIndex('byUserMarina', ['userId', 'marinaId']);
    ops.createIndex('byLaneCreated', ['lane', 'createdAt']);
    db.createObjectStore('blobs', { keyPath: 'id' });
  }
}

function openRaw(version?: number): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const factory = idb();
    if (!factory) {
      reject(new Error('offline: IndexedDB not available'));
      return;
    }
    const req = version === undefined ? factory.open(DB_NAME) : factory.open(DB_NAME, version);
    req.onupgradeneeded = (e) => upgrade(req.result, e.oldVersion);
    req.onsuccess = () => {
      const db = req.result;
      // Another tab deleting or upgrading the database: let it, reopen on next use.
      db.onversionchange = () => {
        db.close();
        dbPromise = null;
      };
      resolve(db);
    };
    req.onerror = () => reject(req.error ?? new Error('offline: open failed'));
    req.onblocked = () => reject(new Error('offline: open blocked'));
  });
}

/** The open database (opened once, reopened after a deletion). */
export function openDb(): Promise<IDBDatabase> {
  if (!dbPromise) {
    dbPromise = openRaw(DB_VERSION).catch(async (err: unknown) => {
      if (err instanceof DOMException && err.name === 'VersionError') {
        // A newer build wrote the database: read it as it is, never write (offline:schemaNewer).
        readOnly = true;
        return openRaw();
      }
      dbPromise = null;
      throw err;
    });
  }
  return dbPromise;
}

function done<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('offline: request failed'));
  });
}

function complete(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onabort = () => reject(tx.error ?? new Error('offline: transaction aborted'));
    tx.onerror = () => reject(tx.error ?? new Error('offline: transaction failed'));
  });
}

async function store(name: StoreName, mode: IDBTransactionMode) {
  if (mode === 'readwrite' && readOnly) throw new OfflineSchemaNewerError();
  const db = await openDb();
  const tx = db.transaction(name, mode);
  return { os: tx.objectStore(name), tx };
}

export async function dbGet<T>(name: StoreName, key: IDBValidKey): Promise<T | undefined> {
  const { os } = await store(name, 'readonly');
  return (await done(os.get(key))) as T | undefined;
}

export async function dbGetAll<T>(name: StoreName): Promise<T[]> {
  const { os } = await store(name, 'readonly');
  return (await done(os.getAll())) as T[];
}

/** Writes a value (keyPath stores: ops, blobs) or a key/value pair (meta, cache). */
export async function dbPut(name: StoreName, value: unknown, key?: IDBValidKey): Promise<void> {
  const { os, tx } = await store(name, 'readwrite');
  if (key === undefined) os.put(value);
  else os.put(value, key);
  await complete(tx);
}

export async function dbDelete(name: StoreName, key: IDBValidKey): Promise<void> {
  const { os, tx } = await store(name, 'readwrite');
  os.delete(key);
  await complete(tx);
}

export async function dbClear(name: StoreName): Promise<void> {
  const { os, tx } = await store(name, 'readwrite');
  os.clear();
  await complete(tx);
}

/**
 * Whether the database may exist: true while a connection is open, else what indexedDB.databases() says (true when
 * the engine cannot tell). Lets a purge skip opening, and so re-creating, a database that logout just deleted.
 */
export async function dbMayExist(): Promise<boolean> {
  const factory = idb();
  if (!factory) return false;
  if (dbPromise) return true;
  if (typeof factory.databases !== 'function') return true;
  try {
    return (await factory.databases()).some((d) => d.name === DB_NAME);
  } catch {
    return true;
  }
}

/** Deletes the whole database (logout). Resolves even when another tab keeps it open (blocked). */
export async function deleteDb(): Promise<void> {
  const factory = idb();
  if (!factory) return;
  if (dbPromise) {
    try {
      (await dbPromise).close();
    } catch {
      // never opened
    }
  }
  dbPromise = null;
  readOnly = false;
  await new Promise<void>((resolve) => {
    const req = factory.deleteDatabase(DB_NAME);
    req.onsuccess = () => resolve();
    req.onerror = () => resolve();
    req.onblocked = () => resolve();
  });
}

/** Test hook: forget the cached connection (fake-indexeddb is reset between tests). */
export function resetDbForTests(): void {
  dbPromise = null;
  readOnly = false;
}
