import { describe, expect, it } from "vitest";
import type { CreateViewEventRequest } from "./types";
import {
  enqueuePendingViewEvent,
  listPendingViewEvents,
  removePendingViewEvent,
  type ViewEventStorage
} from "./viewEventDelivery";

const event: CreateViewEventRequest = {
  video_id: 42,
  scene: "recommend",
  request_id: "req-1",
  event_type: "skip",
  watch_ms: 3_000,
  completed: false,
  event_id: "event-1",
  playback_session_id: "playback-1",
  sequence: 3,
  occurred_at: "2026-07-25T10:00:00Z",
  position_ms: 4_000,
  duration_ms: 100_000
};

describe("view event retry storage", () => {
  it("retains and removes an exact event identity", () => {
    const storage = createMemoryStorage();

    enqueuePendingViewEvent(7, event, storage);
    expect(listPendingViewEvents(7, storage)).toEqual([event]);

    removePendingViewEvent(7, event.event_id!, storage);
    expect(listPendingViewEvents(7, storage)).toEqual([]);
  });

  it("replaces duplicate event IDs instead of growing the queue", () => {
    const storage = createMemoryStorage();

    enqueuePendingViewEvent(7, event, storage);
    enqueuePendingViewEvent(7, { ...event, watch_ms: 4_000 }, storage);

    expect(listPendingViewEvents(7, storage)).toEqual([{ ...event, watch_ms: 4_000 }]);
  });

  it("does not replay one user's pending events as another user", () => {
    const storage = createMemoryStorage();

    enqueuePendingViewEvent(7, event, storage);

    expect(listPendingViewEvents(8, storage)).toEqual([]);
    expect(listPendingViewEvents(7, storage)).toEqual([event]);
  });
});

function createMemoryStorage(): ViewEventStorage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    getItem: (key) => values.get(key) || null,
    key: (index) => [...values.keys()][index] || null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key)
  };
}
