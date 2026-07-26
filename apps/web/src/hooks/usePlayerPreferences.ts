import { useCallback, useRef, useState } from "react";
import {
  DEFAULT_PLAYER_PREFERENCES,
  PlayerPreferencesStore,
  type PlayerPreferences
} from "../player";

export function usePlayerPreferences() {
  const storeRef = useRef<PlayerPreferencesStore | null>(null);
  if (!storeRef.current) storeRef.current = new PlayerPreferencesStore();
  const [preferences, setPreferences] = useState<PlayerPreferences>(
    () => storeRef.current?.load() || { ...DEFAULT_PLAYER_PREFERENCES }
  );

  const updatePreferences = useCallback((patch: Partial<PlayerPreferences>) => {
    const next = storeRef.current?.update(patch) || preferences;
    setPreferences(next);
    return next;
  }, [preferences]);

  return { preferences, updatePreferences };
}
