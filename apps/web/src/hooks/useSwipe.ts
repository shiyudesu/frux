// useSwipe：Feed 滑动/手势切换逻辑（滚轮、指针拖拽、滑动动画）。
// 搬运自 LegacyApp.jsx FeedPage 中 swipe 相关 state/refs 与 handlers，逻辑不变。
import { useEffect, useRef, useState } from "react";
import type * as React from "react";
import { FEED_TRANSITION_MS } from "../constants";

export type SwipeDirection = "next" | "prev";
export type SwipeSettling = "" | "commit" | "cancel";

export interface SwipeState {
  fromIndex: number;
  toIndex: number;
  direction: SwipeDirection;
  height: number;
  offset: number;
  settling: SwipeSettling;
}

interface DragState {
  pointerId: number;
  startY: number;
  fromIndex: number;
  active: boolean;
  direction: "" | SwipeDirection;
  toIndex?: number;
  height: number;
  target: HTMLElement;
}

export function getFeedTrackStyle(swipe: SwipeState | null): { transform: string; transition?: string } {
  if (!swipe) {
    return {
      transform: "translate3d(0, 0, 0)"
    };
  }
  const base = swipe.direction === "prev" ? -swipe.height : 0;
  return {
    transform: `translate3d(0, ${base + swipe.offset}px, 0)`,
    transition: swipe.settling ? `transform ${FEED_TRANSITION_MS}ms cubic-bezier(0.16, 1, 0.3, 1)` : "none"
  };
}

function clampSwipeOffset(direction: SwipeDirection, delta: number, height: number): number {
  if (direction === "next") {
    return Math.max(-height, Math.min(0, delta));
  }
  return Math.min(height, Math.max(0, delta));
}

function isInteractiveTarget(target: EventTarget | null): boolean {
  // 等价于迁移前的 `target?.closest?.(...)`：非 Element 的 target 没有 closest，返回 false
  return target instanceof Element && Boolean(target.closest("button, a, input, textarea, select, [data-ui='details-panel']"));
}

export interface UseSwipeOptions {
  index: number;
  itemsCount: number;
  onIndexChange: (nextIndex: number) => void;
  stageRef: React.RefObject<HTMLElement | null>;
}

export function useSwipe({ index, itemsCount, onIndexChange, stageRef }: UseSwipeOptions) {
  const [swipe, setSwipe] = useState<SwipeState | null>(null);
  const swipeRef = useRef<SwipeState | null>(null);
  const wheelLocked = useRef(false);
  const transitionTimer = useRef<number | null>(null);
  const wheelUnlockTimer = useRef<number | null>(null);
  const dragRef = useRef<DragState | null>(null);

  useEffect(() => {
    return () => {
      if (transitionTimer.current) {
        window.clearTimeout(transitionTimer.current);
      }
      if (wheelUnlockTimer.current) {
        window.clearTimeout(wheelUnlockTimer.current);
      }
    };
  }, []);

  useEffect(() => {
    swipeRef.current = swipe;
  }, [swipe]);

  function getStageHeight() {
    return stageRef.current?.clientHeight || window.innerHeight || 1;
  }

  function cancelSwipe() {
    if (transitionTimer.current) {
      window.clearTimeout(transitionTimer.current);
      transitionTimer.current = null;
    }
    if (wheelUnlockTimer.current) {
      window.clearTimeout(wheelUnlockTimer.current);
      wheelUnlockTimer.current = null;
    }
    wheelLocked.current = false;
    const drag = dragRef.current;
    if (drag?.target.hasPointerCapture(drag.pointerId)) {
      drag.target.releasePointerCapture(drag.pointerId);
    }
    dragRef.current = null;
    swipeRef.current = null;
    setSwipe(null);
  }

  function moveTo(nextIndex: number) {
    if (swipe || nextIndex === index || nextIndex < 0 || nextIndex >= itemsCount) return;
    const direction: SwipeDirection = nextIndex > index ? "next" : "prev";
    const height = getStageHeight();
    if (transitionTimer.current) {
      window.clearTimeout(transitionTimer.current);
    }
    setSwipe({
      fromIndex: index,
      toIndex: nextIndex,
      direction,
      height,
      offset: 0,
      settling: ""
    });
    window.requestAnimationFrame(() => {
      setSwipe((state) =>
        state && state.fromIndex === index && state.toIndex === nextIndex
          ? {
              ...state,
              offset: direction === "next" ? -height : height,
              settling: "commit"
            }
          : state
      );
    });
    transitionTimer.current = window.setTimeout(() => {
      onIndexChange(nextIndex);
      setSwipe(null);
      transitionTimer.current = null;
    }, FEED_TRANSITION_MS);
  }

  function settleSwipe(commit: boolean) {
    const active = swipeRef.current;
    if (!active) return;
    if (transitionTimer.current) {
      window.clearTimeout(transitionTimer.current);
    }
    setSwipe({
      ...active,
      offset: commit ? (active.direction === "next" ? -active.height : active.height) : 0,
      settling: commit ? "commit" : "cancel"
    });
    transitionTimer.current = window.setTimeout(() => {
      if (commit) {
        onIndexChange(active.toIndex);
      }
      setSwipe(null);
      transitionTimer.current = null;
    }, FEED_TRANSITION_MS);
  }

  function handlePointerDown(event: React.PointerEvent<HTMLElement>) {
    if (event.button > 0 || swipe || itemsCount < 2 || isInteractiveTarget(event.target)) return;
    dragRef.current = {
      pointerId: event.pointerId,
      startY: event.clientY,
      fromIndex: index,
      active: false,
      direction: "",
      height: 0,
      target: event.currentTarget
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function handlePointerMove(event: React.PointerEvent<HTMLElement>) {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    const delta = event.clientY - drag.startY;
    if (!drag.active) {
      if (Math.abs(delta) < 8) return;
      const direction: SwipeDirection = delta < 0 ? "next" : "prev";
      const toIndex = direction === "next" ? drag.fromIndex + 1 : drag.fromIndex - 1;
      if (toIndex < 0 || toIndex >= itemsCount) {
        return;
      }
      const height = getStageHeight();
      dragRef.current = {
        ...drag,
        active: true,
        direction,
        toIndex,
        height
      };
      setSwipe({
        fromIndex: drag.fromIndex,
        toIndex,
        direction,
        height,
        offset: clampSwipeOffset(direction, delta, height),
        settling: ""
      });
      event.preventDefault();
      return;
    }

    setSwipe((state) =>
      state
        ? {
            ...state,
            offset: clampSwipeOffset(state.direction, delta, state.height),
            settling: ""
          }
        : state
    );
    event.preventDefault();
  }

  function handlePointerEnd(event: React.PointerEvent<HTMLElement>) {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    dragRef.current = null;
    const active = swipeRef.current;
    if (!drag.active || !active) return;
    const threshold = Math.min(active.height * 0.24, 220);
    settleSwipe(Math.abs(active.offset) >= threshold);
  }

  function goNext() {
    moveTo(Math.min(itemsCount - 1, index + 1));
  }

  function goPrev() {
    moveTo(Math.max(0, index - 1));
  }

  function handleWheel(event: React.WheelEvent<HTMLElement>) {
    if (Math.abs(event.deltaY) < 32 || wheelLocked.current || swipe || itemsCount < 2) return;
    wheelLocked.current = true;
    if (event.deltaY > 0) {
      goNext();
    } else {
      goPrev();
    }
    wheelUnlockTimer.current = window.setTimeout(() => {
      wheelLocked.current = false;
      wheelUnlockTimer.current = null;
    }, 420);
  }

  return {
    swipe,
    setSwipe,
    cancelSwipe,
    moveTo,
    handlePointerDown,
    handlePointerMove,
    handlePointerEnd,
    handleWheel
  };
}
