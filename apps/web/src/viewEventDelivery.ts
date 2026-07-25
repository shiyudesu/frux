import type { CreateViewEventRequest } from "./types";

const PENDING_VIEW_EVENTS_KEY = "gcfeed.pending-view-events.v1";
const MAX_PENDING_VIEW_EVENTS = 50;

export interface ViewEventStorage {
  readonly length: number;
  getItem: (key: string) => string | null;
  key: (index: number) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem: (key: string) => void;
}

interface PendingViewEvent {
  user_id: number;
  event: CreateViewEventRequest;
}

export function enqueuePendingViewEvent(
  userID: number,
  event: CreateViewEventRequest,
  storage: ViewEventStorage | null = browserPersistentStorage()
): void {
  if (!storage || userID <= 0 || !event.event_id) return;
  try {
    storage.setItem(eventStorageKey(userID, event.event_id), JSON.stringify({ user_id: userID, event }));
    const pending = readPendingViewEvents(storage, userID);
    for (const stale of pending.slice(0, Math.max(0, pending.length - MAX_PENDING_VIEW_EVENTS))) {
      if (stale.event.event_id) {
        storage.removeItem(eventStorageKey(userID, stale.event.event_id));
      }
    }
  } catch {
    // Playback remains independent from best-effort retry storage.
  }
}

export function removePendingViewEvent(
  userID: number,
  eventID: string,
  storage: ViewEventStorage | null = browserPersistentStorage()
): void {
  if (!storage || userID <= 0 || !eventID) return;
  try {
    storage.removeItem(eventStorageKey(userID, eventID));
  } catch {
    // Playback remains independent from best-effort retry storage.
  }
}

export function listPendingViewEvents(
  userID: number,
  storage: ViewEventStorage | null = browserPersistentStorage()
): CreateViewEventRequest[] {
  if (!storage || userID <= 0) return [];
  return readPendingViewEvents(storage, userID).map((item) => item.event);
}

function browserPersistentStorage(): ViewEventStorage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function readPendingViewEvents(storage: ViewEventStorage, userID: number): PendingViewEvent[] {
  const events: PendingViewEvent[] = [];
  const prefix = userStoragePrefix(userID);
  for (let index = 0; index < storage.length; index += 1) {
    const key = storage.key(index);
    if (!key?.startsWith(prefix)) continue;
    try {
      const value: unknown = JSON.parse(storage.getItem(key) || "null");
      if (isPendingViewEvent(value) && value.user_id === userID) {
        events.push(value);
      }
    } catch {
      storage.removeItem(key);
    }
  }
  return events.sort(comparePendingViewEvents);
}

function userStoragePrefix(userID: number): string {
  return `${PENDING_VIEW_EVENTS_KEY}.${userID}.`;
}

function eventStorageKey(userID: number, eventID: string): string {
  return `${userStoragePrefix(userID)}${encodeURIComponent(eventID)}`;
}

function comparePendingViewEvents(left: PendingViewEvent, right: PendingViewEvent): number {
  const occurredOrder = String(left.event.occurred_at || "").localeCompare(String(right.event.occurred_at || ""));
  if (occurredOrder !== 0) return occurredOrder;
  return Number(left.event.sequence || 0) - Number(right.event.sequence || 0);
}

function isPendingViewEvent(value: unknown): value is PendingViewEvent {
  if (!isRecord(value) || typeof value.user_id !== "number" || !isRecord(value.event)) return false;
  return (
    typeof value.event.event_id === "string" &&
    typeof value.event.video_id === "number" &&
    typeof value.event.scene === "string" &&
    typeof value.event.event_type === "string"
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
