import { useCallback, useEffect, useRef, useState } from "react";

export const CHAT_POLL_BASE_INTERVAL_MS = 5_000;
export const CHAT_POLL_MAX_INTERVAL_MS = 30_000;

export function useChatPolling(
  enabled: boolean,
  onPoll: () => Promise<void>
) {
  const [degraded, setDegraded] = useState(false);
  const onPollRef = useRef(onPoll);
  const timerRef = useRef<number | null>(null);
  const failureCountRef = useRef(0);
  const activeRef = useRef(false);
  const runRef = useRef<() => void>();
  onPollRef.current = onPoll;

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const schedule = useCallback((delay: number) => {
    clearTimer();
    if (!activeRef.current) return;
    timerRef.current = window.setTimeout(() => {
      runRef.current?.();
    }, delay);
  }, [clearTimer]);

  const run = useCallback(async () => {
    if (!activeRef.current || document.visibilityState === "hidden") return;
    try {
      await onPollRef.current();
      failureCountRef.current = 0;
      setDegraded(false);
      schedule(CHAT_POLL_BASE_INTERVAL_MS);
    } catch {
      failureCountRef.current += 1;
      setDegraded(true);
      const delay = Math.min(
        CHAT_POLL_MAX_INTERVAL_MS,
        CHAT_POLL_BASE_INTERVAL_MS * (2 ** Math.min(failureCountRef.current, 3))
      );
      schedule(delay);
    }
  }, [schedule]);
  runRef.current = () => {
    void run();
  };

  useEffect(() => {
    activeRef.current = enabled;
    clearTimer();
    if (!enabled) {
      failureCountRef.current = 0;
      setDegraded(false);
      return undefined;
    }
    const handleVisibility = () => {
      if (document.visibilityState === "visible") {
        failureCountRef.current = 0;
        void run();
      } else {
        clearTimer();
      }
    };
    document.addEventListener("visibilitychange", handleVisibility);
    if (document.visibilityState !== "hidden") void run();
    return () => {
      activeRef.current = false;
      clearTimer();
      document.removeEventListener("visibilitychange", handleVisibility);
    };
  }, [clearTimer, enabled, run]);

  return {
    degraded,
    retry: () => {
      failureCountRef.current = 0;
      if (activeRef.current) void run();
    }
  };
}
