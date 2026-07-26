import { describe, expect, it } from "vitest";
import {
  PLAYER_PREFERENCES_STORAGE_KEY,
  PlayerPreferencesStore,
  isPlayerPreferences,
  sanitizePlayerPreferences,
  type PlayerPreferenceStorage
} from "./preferences";

class MemoryStorage implements PlayerPreferenceStorage {
  readonly values = new Map<string, string>();

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }

  removeItem(key: string): void {
    this.values.delete(key);
  }
}

describe("PlayerPreferencesStore", () => {
  it("persists validated quality, speed, and continuous-play preferences", () => {
    const storage = new MemoryStorage();
    const store = new PlayerPreferencesStore(storage);
    store.save({ quality: "dash-representation-720", playbackRate: 1.5, continuousPlay: true });

    expect(store.load()).toEqual({
      quality: "dash-representation-720",
      playbackRate: 1.5,
      continuousPlay: true
    });
    expect(JSON.parse(storage.values.get(PLAYER_PREFERENCES_STORAGE_KEY) ?? "{}")).toEqual(store.load());
  });

  it("removes malformed stored JSON and returns compatibility defaults", () => {
    const storage = new MemoryStorage();
    storage.values.set(
      PLAYER_PREFERENCES_STORAGE_KEY,
      JSON.stringify({ quality: "<script>", playbackRate: 12, continuousPlay: "yes" })
    );
    const store = new PlayerPreferencesStore(storage);

    expect(store.load()).toEqual({ quality: "auto", playbackRate: 1, continuousPlay: false });
    expect(storage.values.has(PLAYER_PREFERENCES_STORAGE_KEY)).toBe(false);
  });

  it("sanitizes programmatic updates and validates exact supported rates", () => {
    expect(
      sanitizePlayerPreferences({ quality: "bad value", playbackRate: 1.1, continuousPlay: true })
    ).toEqual({ quality: "auto", playbackRate: 1, continuousPlay: true });
    expect(isPlayerPreferences({ quality: "auto", playbackRate: 2, continuousPlay: false })).toBe(true);
    expect(isPlayerPreferences({ quality: "auto", playbackRate: 2.5, continuousPlay: false })).toBe(false);
  });
});
