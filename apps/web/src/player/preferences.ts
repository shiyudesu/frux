import { DEFAULT_PLAYER_PREFERENCES } from "./types";
import type { PlayerPreferences, QualitySelection } from "./types";

export const PLAYER_PREFERENCES_STORAGE_KEY = "frux.player.preferences.v1";
export const SUPPORTED_PLAYBACK_RATES = [0.5, 0.75, 1, 1.25, 1.5, 2] as const;

export interface PlayerPreferenceStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export class PlayerPreferencesStore {
  constructor(
    private readonly storage: PlayerPreferenceStorage | undefined = browserStorage(),
    private readonly key = PLAYER_PREFERENCES_STORAGE_KEY
  ) {}

  load(): PlayerPreferences {
    if (!this.storage) return { ...DEFAULT_PLAYER_PREFERENCES };
    try {
      const raw = this.storage.getItem(this.key);
      if (!raw) return { ...DEFAULT_PLAYER_PREFERENCES };
      const parsed: unknown = JSON.parse(raw);
      if (!isPlayerPreferences(parsed)) {
        this.storage.removeItem(this.key);
        return { ...DEFAULT_PLAYER_PREFERENCES };
      }
      return parsed;
    } catch {
      return { ...DEFAULT_PLAYER_PREFERENCES };
    }
  }

  save(preferences: PlayerPreferences): PlayerPreferences {
    const validated = sanitizePlayerPreferences(preferences);
    try {
      this.storage?.setItem(this.key, JSON.stringify(validated));
    } catch {
      return validated;
    }
    return validated;
  }

  update(patch: Partial<PlayerPreferences>): PlayerPreferences {
    return this.save({ ...this.load(), ...patch });
  }

  clear(): void {
    try {
      this.storage?.removeItem(this.key);
    } catch {
      return;
    }
  }
}

export function isPlayerPreferences(value: unknown): value is PlayerPreferences {
  if (!isRecord(value)) return false;
  return (
    isQualitySelection(value.quality) &&
    isPlaybackRate(value.playbackRate) &&
    typeof value.continuousPlay === "boolean"
  );
}

export function sanitizePlayerPreferences(value: Partial<PlayerPreferences>): PlayerPreferences {
  return {
    quality: isQualitySelection(value.quality) ? value.quality : DEFAULT_PLAYER_PREFERENCES.quality,
    playbackRate: isPlaybackRate(value.playbackRate)
      ? value.playbackRate
      : DEFAULT_PLAYER_PREFERENCES.playbackRate,
    continuousPlay:
      typeof value.continuousPlay === "boolean"
        ? value.continuousPlay
        : DEFAULT_PLAYER_PREFERENCES.continuousPlay
  };
}

export function isQualitySelection(value: unknown): value is QualitySelection {
  if (value === "auto") return true;
  return typeof value === "string" && value.length > 0 && value.length <= 128 && /^[a-zA-Z0-9._:-]+$/.test(value);
}

export function isPlaybackRate(value: unknown): value is (typeof SUPPORTED_PLAYBACK_RATES)[number] {
  return (
    typeof value === "number" &&
    SUPPORTED_PLAYBACK_RATES.some((supportedRate) => supportedRate === value)
  );
}

function browserStorage(): PlayerPreferenceStorage | undefined {
  if (typeof localStorage === "undefined") return undefined;
  return localStorage;
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null;
}
