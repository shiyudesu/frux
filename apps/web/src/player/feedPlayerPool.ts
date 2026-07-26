import {
  feedPreloadResourceKey,
  type AcquiredFeedPreloadResource,
  type EffectiveFeedPreloadPolicy,
  type FeedPreloadCandidate,
  type FeedPreloadMediaResource,
  type FeedPreloadReadiness,
  type FeedPreloadResourceKey
} from "../feedPreload";

export const MAX_FEED_PLAYER_POOL_SLOTS = 3;

export type FeedPlayerPoolRole = "previous" | "current" | "next";

export interface FeedPlayerPoolController {
  sync(candidates: FeedPreloadCandidate[], policy: EffectiveFeedPreloadPolicy): void;
  acquireCandidate(
    candidate: FeedPreloadCandidate,
    policy: EffectiveFeedPreloadPolicy
  ): AcquiredFeedPreloadResource | undefined;
  destroy(): void;
}

export interface FeedPlayerPoolResource {
  readonly resourceKey: string;
  readonly videoID: number;
  readonly role: FeedPlayerPoolRole;
  readonly candidate: FeedPreloadCandidate;
  readonly media: FeedPreloadMediaResource;
  readonly readiness: FeedPreloadReadiness;
  readonly bufferedMs: number;
}

interface PoolEntry {
  resourceKey: string;
  role: FeedPlayerPoolRole;
  candidate: FeedPreloadCandidate;
  handle: AcquiredFeedPreloadResource;
}

interface SelectedCandidate {
  role: FeedPlayerPoolRole;
  candidate: FeedPreloadCandidate;
}

export class FeedPlayerPool {
  private readonly entries = new Map<string, PoolEntry>();
  private readonly retiredGenerations = new Set<string>();
  private activeGeneration: string | null = null;
  private destroyed = false;

  constructor(
    private readonly controller: FeedPlayerPoolController
  ) {}

  synchronize(
    candidates: readonly FeedPreloadCandidate[],
    policy: EffectiveFeedPreloadPolicy
  ): readonly FeedPlayerPoolResource[] {
    if (this.destroyed) return [];
    const boundedPolicy = boundPolicy(policy);
    if (!candidates.length) {
      this.releaseAll();
      this.activeGeneration = null;
      this.controller.sync([], boundedPolicy);
      return [];
    }

    const activeCandidate = candidates.find((candidate) => candidate.role === "active");
    if (!activeCandidate) return this.listResources();
    const generation = activeCandidate.key.generation;
    if (this.retiredGenerations.has(generation)) return this.listResources();

    if (this.activeGeneration !== generation) {
      if (this.activeGeneration) this.retireGeneration(this.activeGeneration);
      this.releaseAll();
      this.activeGeneration = generation;
    }

    const selected = selectPoolCandidates(
      candidates.filter((candidate) => candidate.key.generation === generation),
      boundedPolicy.maxResources
    );
    const selectedKeys = new Set(selected.map(({ candidate }) => feedPreloadResourceKey(candidate.key)));

    for (const [resourceKey, entry] of [...this.entries]) {
      if (selectedKeys.has(resourceKey)) continue;
      entry.handle.release();
      this.entries.delete(resourceKey);
    }

    this.controller.sync(
      selected.map(({ candidate }) => candidate),
      boundedPolicy
    );

    const acquisitionOrder = [...selected].sort(
      (left, right) => acquisitionPriority(left.role) - acquisitionPriority(right.role)
    );
    for (const { role, candidate } of acquisitionOrder) {
      const resourceKey = feedPreloadResourceKey(candidate.key);
      const existing = this.entries.get(resourceKey);
      if (existing) {
        existing.role = role;
        existing.candidate = candidate;
        continue;
      }
      if (this.entries.size >= MAX_FEED_PLAYER_POOL_SLOTS) break;
      const handle = this.controller.acquireCandidate(candidate, boundedPolicy);
      if (!handle) continue;
      this.entries.set(resourceKey, { resourceKey, role, candidate, handle });
    }

    return this.listResources();
  }

  get size(): number {
    return this.entries.size;
  }

  get generation(): string | null {
    return this.activeGeneration;
  }

  getResourceByKey(key: FeedPreloadResourceKey | string): FeedPlayerPoolResource | undefined {
    const resourceKey = typeof key === "string" ? key : feedPreloadResourceKey(key);
    const entry = this.entries.get(resourceKey);
    return entry ? resourceView(entry) : undefined;
  }

  getResourceByVideoID(
    videoID: number,
    role?: FeedPlayerPoolRole
  ): FeedPlayerPoolResource | undefined {
    const matches = [...this.entries.values()].filter(
      (entry) => entry.candidate.key.videoID === videoID && (role === undefined || entry.role === role)
    );
    matches.sort((left, right) => rolePriority(left.role) - rolePriority(right.role));
    return matches[0] ? resourceView(matches[0]) : undefined;
  }

  getResourceByRole(role: FeedPlayerPoolRole): FeedPlayerPoolResource | undefined {
    const entry = [...this.entries.values()].find((candidate) => candidate.role === role);
    return entry ? resourceView(entry) : undefined;
  }

  listResources(): readonly FeedPlayerPoolResource[] {
    return [...this.entries.values()]
      .sort((left, right) => rolePriority(left.role) - rolePriority(right.role))
      .map(resourceView);
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.releaseAll();
    this.activeGeneration = null;
    this.retiredGenerations.clear();
    this.controller.destroy();
  }

  private releaseAll(): void {
    for (const entry of this.entries.values()) entry.handle.release();
    this.entries.clear();
  }

  private retireGeneration(generation: string): void {
    this.retiredGenerations.add(generation);
    while (this.retiredGenerations.size > 16) {
      const oldest = this.retiredGenerations.values().next().value;
      if (oldest === undefined) break;
      this.retiredGenerations.delete(oldest);
    }
  }
}

function selectPoolCandidates(
  candidates: readonly FeedPreloadCandidate[],
  slotLimit: number
): readonly SelectedCandidate[] {
  const active = candidates
    .filter((candidate) => candidate.role === "active")
    .sort((left, right) => left.feedIndex - right.feedIndex)[0];
  if (!active) return [];
  const previous = candidates
    .filter((candidate) => candidate.role === "previous" && candidate.feedIndex < active.feedIndex)
    .sort((left, right) => right.feedIndex - left.feedIndex)[0];
  const next = candidates
    .filter((candidate) => candidate.role === "forward" && candidate.feedIndex > active.feedIndex)
    .sort((left, right) => left.feedIndex - right.feedIndex)[0];
  const ordered: SelectedCandidate[] = [];
  if (previous) ordered.push({ role: "previous", candidate: previous });
  ordered.push({ role: "current", candidate: active });
  if (next) ordered.push({ role: "next", candidate: next });

  const seen = new Set<string>();
  const unique = ordered.filter(({ candidate }) => {
    const resourceKey = feedPreloadResourceKey(candidate.key);
    if (seen.has(resourceKey)) return false;
    seen.add(resourceKey);
    return true;
  });
  if (unique.length <= slotLimit) return unique;
  return unique
    .sort((left, right) => acquisitionPriority(left.role) - acquisitionPriority(right.role))
    .slice(0, slotLimit)
    .sort((left, right) => rolePriority(left.role) - rolePriority(right.role));
}

function resourceView(entry: PoolEntry): FeedPlayerPoolResource {
  return {
    resourceKey: entry.resourceKey,
    videoID: entry.candidate.key.videoID,
    get role() {
      return entry.role;
    },
    get candidate() {
      return entry.candidate;
    },
    media: entry.handle.media,
    get readiness() {
      return entry.handle.readiness;
    },
    get bufferedMs() {
      return entry.handle.bufferedMs;
    }
  };
}

function boundPolicy(policy: EffectiveFeedPreloadPolicy): EffectiveFeedPreloadPolicy {
  const configuredMax = Number.isFinite(policy.maxResources)
    ? Math.trunc(policy.maxResources)
    : MAX_FEED_PLAYER_POOL_SLOTS;
  return {
    ...policy,
    maxResources: Math.min(MAX_FEED_PLAYER_POOL_SLOTS, Math.max(1, configuredMax))
  };
}

function rolePriority(role: FeedPlayerPoolRole): number {
  if (role === "previous") return 0;
  if (role === "current") return 1;
  return 2;
}

function acquisitionPriority(role: FeedPlayerPoolRole): number {
  if (role === "current") return 0;
  if (role === "next") return 1;
  return 2;
}
