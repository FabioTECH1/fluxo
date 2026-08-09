export interface CachedLogSource {
  id: string;
  label: string;
  path: string;
  exists: boolean;
}

export interface LogViewSnapshot {
  sources: CachedLogSource[];
  selectedLog: string;
  lines: string[];
  updatedAt: number | null;
}

interface CacheEntry {
  snapshot: LogViewSnapshot;
  savedAt: number;
}

const snapshots = new Map<string, CacheEntry>();
const MAX_ENTRIES = 20;
const MAX_AGE_MS = 300_000;

export const getLogViewSnapshot = (key: string): LogViewSnapshot | null => {
  const entry = snapshots.get(key);
  if (!entry) return null;
  if (Date.now() - entry.savedAt > MAX_AGE_MS) {
    snapshots.delete(key);
    return null;
  }

  // Move recent entries to the end so the size limit behaves as an LRU cache.
  snapshots.delete(key);
  snapshots.set(key, entry);
  return {
    sources: entry.snapshot.sources.map(source => ({ ...source })),
    selectedLog: entry.snapshot.selectedLog,
    lines: [...entry.snapshot.lines],
    updatedAt: entry.snapshot.updatedAt,
  };
};

export const setLogViewSnapshot = (key: string, snapshot: LogViewSnapshot) => {
  snapshots.delete(key);
  snapshots.set(key, {
    snapshot: {
      sources: snapshot.sources.map(source => ({ ...source })),
      selectedLog: snapshot.selectedLog,
      lines: [...snapshot.lines],
      updatedAt: snapshot.updatedAt,
    },
    savedAt: Date.now(),
  });

  while (snapshots.size > MAX_ENTRIES) {
    const oldestKey = snapshots.keys().next().value;
    if (typeof oldestKey !== 'string') break;
    snapshots.delete(oldestKey);
  }
};

export const clearLogViewSnapshots = () => snapshots.clear();
