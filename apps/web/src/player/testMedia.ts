import type { MediaErrorLike, PlayerMediaElement, TimeRangesLike } from "./types";

export class FakeTimeRanges implements TimeRangesLike {
  constructor(private readonly ranges: readonly (readonly [number, number])[] = []) {}

  get length(): number {
    return this.ranges.length;
  }

  start(index: number): number {
    return this.ranges[index]?.[0] ?? 0;
  }

  end(index: number): number {
    return this.ranges[index]?.[1] ?? 0;
  }
}

export class FakePlayerMedia implements PlayerMediaElement {
  currentTime = 0;
  duration = 0;
  paused = true;
  ended = false;
  muted = true;
  volume = 1;
  playbackRate = 1;
  readyState = 0;
  buffered: TimeRangesLike = new FakeTimeRanges();
  error: MediaErrorLike | null = null;
  currentSrc = "";
  src = "";
  preload = "";
  loop = false;
  playsInline = false;
  loadCount = 0;
  pauseCount = 0;
  playResult: Promise<void> = Promise.resolve();
  private readonly listeners = new Map<string, Set<EventListener>>();

  load(): void {
    this.loadCount += 1;
    this.currentSrc = this.src;
  }

  play(): Promise<void> {
    this.paused = false;
    return this.playResult;
  }

  pause(): void {
    this.pauseCount += 1;
    this.paused = true;
  }

  removeAttribute(name: string): void {
    if (name === "src") {
      this.src = "";
      this.currentSrc = "";
    }
  }

  addEventListener(type: string, listener: EventListener): void {
    const listeners = this.listeners.get(type) ?? new Set<EventListener>();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: EventListener): void {
    this.listeners.get(type)?.delete(listener);
  }

  emit(type: string): void {
    for (const listener of this.listeners.get(type) ?? []) listener(new Event(type));
  }

  listenerCount(): number {
    let count = 0;
    for (const listeners of this.listeners.values()) count += listeners.size;
    return count;
  }
}
