import { ASSET_ACTIVE_COOKIE_NAME, TOKEN_KEY, USER_KEY } from "./constants";
import {
  ApiError,
  apiRequest,
  isInvalidRefreshSessionError,
  isRefreshSessionReplayedError,
  isSupersededRefreshSessionError,
  type ConsumerAuthController
} from "./api/client";
import { parseStoredUser } from "./types";
import type { SessionUser, TokenResponse } from "./types";

export type ConsumerSessionStatus = "bootstrapping" | "authenticated" | "unauthenticated";

export interface ConsumerSessionSnapshot {
  token: string;
  user: SessionUser | null;
  status: ConsumerSessionStatus;
  expiresAt: number;
}

interface BroadcastChannelLike {
  close(): void;
  postMessage(message: unknown): void;
  addEventListener?: (type: "message", listener: (event: MessageEvent<unknown>) => void) => void;
  removeEventListener?: (type: "message", listener: (event: MessageEvent<unknown>) => void) => void;
  onmessage?: ((event: MessageEvent<unknown>) => void) | null;
}

interface ConsumerSessionCoordinatorOptions {
  storage?: Storage | null;
  sessionStorage?: Storage | null;
  window?: Window | null;
  createBroadcastChannel?: (name: string) => BroadcastChannelLike | null;
  now?: () => number;
  runCredentialLock?: <T>(operation: () => Promise<T>) => Promise<T>;
}

interface LogoutSignal {
  type: "logout";
  at: number;
}

type Listener = (snapshot: ConsumerSessionSnapshot) => void;

const CONSUMER_SESSION_BROADCAST_CHANNEL = "frux.consumer.session.v1";
const CONSUMER_CREDENTIAL_LOCK = "frux.consumer.credential.v1";
export const CONSUMER_SESSION_SIGNAL_KEY = "frux.consumer.session.signal.v1";
export const CONSUMER_LOGOUT_PENDING_KEY = "frux.consumer.logout.pending.v1";

export class ConsumerSessionCoordinator implements ConsumerAuthController {
  private readonly storage: Storage | null;
  private readonly adminStorage: Storage | null;
  private readonly windowValue: Window | null;
  private readonly createBroadcastChannel: (name: string) => BroadcastChannelLike | null;
  private readonly now: () => number;
  private readonly runCredentialLock: <T>(
    operation: () => Promise<T>
  ) => Promise<T>;
  private readonly listeners = new Set<Listener>();
  private readonly handleStorageEvent = (event: StorageEvent) => {
    if (event.key !== CONSUMER_SESSION_SIGNAL_KEY || !event.newValue) return;
    const signal = parseLogoutSignal(event.newValue);
    if (!signal) return;
    this.clearAuth({ broadcast: false });
  };
  private readonly handleChannelMessage = (event: MessageEvent<unknown>) => {
    if (!isLogoutSignal(event.data)) return;
    this.clearAuth({ broadcast: false });
  };
  private snapshot: ConsumerSessionSnapshot;
  private bootstrapPromise: Promise<void> | null = null;
  private refreshPromise: Promise<string | null> | null = null;
  private refreshGeneration = 0;
  private sessionEpoch = 0;
  private readonly tokenEpochs = new Map<string, number>();
  private credentialMutationTail: Promise<void> = Promise.resolve();
  private refreshBlock: Promise<void> | null = null;
  private releaseRefreshBlock: (() => void) | null = null;
  private broadcastChannel: BroadcastChannelLike | null = null;
  private listenersInstalled = false;

  constructor(options: ConsumerSessionCoordinatorOptions = {}) {
    this.storage = options.storage ?? storageOrNull(() => localStorage);
    this.adminStorage = options.sessionStorage ?? storageOrNull(() => sessionStorage);
    this.windowValue = options.window ?? windowOrNull();
    this.createBroadcastChannel = options.createBroadcastChannel ?? defaultBroadcastChannelFactory;
    this.now = options.now ?? (() => Date.now());
    this.runCredentialLock = options.runCredentialLock ?? defaultCredentialLock;
    this.snapshot = {
      token: "",
      user: this.readCachedUser(),
      status: "bootstrapping",
      expiresAt: 0
    };
  }

  getSnapshot(): ConsumerSessionSnapshot {
    return this.snapshot;
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener);
    listener(this.snapshot);
    return () => {
      this.listeners.delete(listener);
    };
  }

  getAccessToken(): string {
    return this.snapshot.token;
  }

  getAccessExpiresAt(): number {
    return this.snapshot.expiresAt;
  }

  getSessionEpoch(): number {
    return this.sessionEpoch;
  }

  getTokenEpoch(token: string): number | null {
    return this.tokenEpochs.get(token) ?? null;
  }

  async bootstrap(): Promise<void> {
    if (this.bootstrapPromise) return this.bootstrapPromise;
    const generation = this.refreshGeneration;
    this.deleteLegacyAccessToken();
    this.setSnapshot({
      token: "",
      user: this.readCachedUser(),
      status: "bootstrapping",
      expiresAt: 0
    });
    if (this.safeStorageGet(CONSUMER_LOGOUT_PENDING_KEY) === "1") {
      this.setSnapshot({
        token: "",
        user: null,
        status: "unauthenticated",
        expiresAt: 0
      });
      this.setAssetAccessActive(false);
      this.bootstrapPromise = this.runCredentialLock(
        () => this.retryPendingLogout()
      ).finally(() => {
        this.bootstrapPromise = null;
      });
      return this.bootstrapPromise;
    }
    this.bootstrapPromise = this.runCredentialLock(
      () => this.runBootstrap(generation)
    ).finally(() => {
      this.bootstrapPromise = null;
    });
    return this.bootstrapPromise;
  }

  setAuth(token: string, user: SessionUser | null, expiresInSeconds = 0): void {
    this.refreshGeneration += 1;
    this.sessionEpoch += 1;
    if (!token) {
      this.clearAuth();
      return;
    }
    this.setAuthenticated(token, user, expiresInSeconds);
  }

  beginLogout(): void {
    this.safeStorageSet(CONSUMER_LOGOUT_PENDING_KEY, "1");
    this.clearAuth();
  }

  completeLogout(): void {
    this.safeStorageRemove(CONSUMER_LOGOUT_PENDING_KEY);
  }

  async runCredentialMutation<T>(
    operation: () => Promise<T>,
    requireAuthenticated = false
  ): Promise<T> {
    const initiatingEpoch = this.sessionEpoch;
    const initiatingUserID = this.snapshot.user?.id ?? 0;
    let releaseTurn!: () => void;
    const previousTurn = this.credentialMutationTail;
    this.credentialMutationTail = new Promise<void>((resolve) => {
      releaseTurn = resolve;
    });
    await previousTurn;

    this.refreshBlock = new Promise<void>((resolve) => {
      this.releaseRefreshBlock = resolve;
    });
    try {
      const pending: Promise<unknown>[] = [];
      if (this.bootstrapPromise) pending.push(this.bootstrapPromise);
      if (this.refreshPromise) pending.push(this.refreshPromise);
      await Promise.allSettled(pending);
      return await this.runCredentialLock(async () => {
        this.assertMutationIdentity(
          requireAuthenticated, initiatingEpoch, initiatingUserID
        );
        if (requireAuthenticated &&
          (!this.snapshot.token || this.snapshot.expiresAt <= this.now() + 30_000)) {
          const refreshed = await this.performRefresh(
            false, this.refreshGeneration
          );
          if (!refreshed) {
            throw new ApiError(
              "authentication required", 401, "AUTH_INVALID_ACCESS_TOKEN"
            );
          }
          this.assertMutationIdentity(true, initiatingEpoch, initiatingUserID);
        }
        return operation();
      });
    } finally {
      this.releaseRefreshBlock?.();
      this.releaseRefreshBlock = null;
      this.refreshBlock = null;
      releaseTurn();
    }

  }

  replaceAccessToken(token: string, expiresInSeconds: number): void {
    this.setAuth(token, this.snapshot.user, expiresInSeconds);
  }

  updateUser(expectedToken: string, user: SessionUser): void {
    if (!expectedToken || !this.snapshot.token) return;
    if (this.snapshot.user && this.snapshot.user.id !== user.id) return;
    this.setSnapshot({ ...this.snapshot, user });
    this.writeCachedUser(user);
  }

  clearAuth(options: { broadcast?: boolean } = {}): void {
    this.refreshGeneration += 1;
    this.sessionEpoch += 1;
    const broadcast = options.broadcast !== false;
    this.setSnapshot({
      token: "",
      user: null,
      status: "unauthenticated",
      expiresAt: 0
    });
    this.setAssetAccessActive(false);
    this.writeCachedUser(null);
    if (broadcast) this.broadcastLogout();
  }

  async refreshAccessToken(): Promise<string | null> {
    const generation = this.refreshGeneration;
    while (this.refreshBlock) await this.refreshBlock;
    if (this.refreshPromise) return this.refreshPromise;
    this.refreshPromise = this.runCredentialLock(
      () => this.performRefresh(false, generation)
    ).finally(() => {
      this.refreshPromise = null;
    });
    return this.refreshPromise;
  }

  start(): void {
    if (this.listenersInstalled) return;
    this.listenersInstalled = true;
    this.installCrossTabListeners();
  }

  stop(): void {
    if (!this.listenersInstalled) return;
    this.listenersInstalled = false;
    this.windowValue?.removeEventListener("storage", this.handleStorageEvent);
    if (this.broadcastChannel?.removeEventListener) {
      this.broadcastChannel.removeEventListener("message", this.handleChannelMessage);
    } else if (this.broadcastChannel) {
      this.broadcastChannel.onmessage = null;
    }
    this.broadcastChannel?.close();
    this.broadcastChannel = null;
  }

  dispose(): void {
    this.stop();
    this.listeners.clear();
  }

  private async runBootstrap(generation: number): Promise<void> {
    try {
      await this.performRefresh(true, generation);
    } catch {
      this.setSnapshot({
        token: "",
        user: this.snapshot.user,
        status: "unauthenticated",
        expiresAt: 0
      });
    }

  }

  private async retryPendingLogout(): Promise<void> {
    try {
      await apiRequest<void>("/api/sessions/current", {
        method: "DELETE",
        credentials: "same-origin",
        cache: "no-store",
        retryAuth: false
      });
      this.safeStorageRemove(CONSUMER_LOGOUT_PENDING_KEY);
    } catch {
      // Keep the marker so a later page load retries durable revocation.
    }
  }

  private async performRefresh(
    bootstrap: boolean,
    generation: number
  ): Promise<string | null> {
    if (this.refreshInvalidated(generation)) return null;
    const cachedUser = this.snapshot.user;
    for (let attempt = 0; attempt < 4; attempt += 1) {
      try {
        const tokenResponse = await apiRequest<TokenResponse>("/api/sessions/current/refresh", {
          method: "POST",
          credentials: "same-origin",
          cache: "no-store",
          retryAuth: false
        });
        if (this.refreshInvalidated(generation)) return null;
        const profile = await apiRequest<SessionUser>("/api/users/me", {
          token: tokenResponse.access_token,
          cache: "no-store",
          retryAuth: false
        });
        if (this.refreshInvalidated(generation)) return null;
        if (cachedUser && cachedUser.id !== profile.id) {
          this.sessionEpoch += 1;
          this.refreshGeneration += 1;
        }
        this.setAuthenticated(
          tokenResponse.access_token,
          profile,
          tokenResponse.expires_in_seconds
        );
        return tokenResponse.access_token;
      } catch (error) {
        if (isSupersededRefreshSessionError(error) && attempt < 3) {
          await waitForRefreshWinner(attempt);
          if (this.refreshInvalidated(generation)) return null;
          continue;
        }

        if (isInvalidRefreshSessionError(error) || isRefreshSessionReplayedError(error)) {
          this.clearAuth({ broadcast: false });
          return null;
        }
        if (bootstrap) {
          this.setSnapshot({
            token: "",
            user: cachedUser,
            status: "unauthenticated",
            expiresAt: 0
          });
          return null;
        }
        throw error;
      }
    }
    return null;
  }

  private refreshInvalidated(generation: number): boolean {
    return generation !== this.refreshGeneration ||
      this.safeStorageGet(CONSUMER_LOGOUT_PENDING_KEY) === "1";
  }

  private assertMutationIdentity(
    required: boolean,
    initiatingEpoch: number,
    initiatingUserID: number
  ): void {
    if (!required) return;
    if (this.sessionEpoch !== initiatingEpoch ||
      !this.snapshot.user ||
      this.snapshot.user.id !== initiatingUserID) {
      throw new ApiError(
        "session identity changed", 409, "AUTH_SESSION_CHANGED"
      );
    }
  }

  private setAuthenticated(token: string, user: SessionUser | null, expiresInSeconds: number): void {
    this.rememberTokenEpoch(token);
    this.setSnapshot({
      token,
      user,
      status: token ? "authenticated" : "unauthenticated",
      expiresAt: this.now() + Math.max(1, expiresInSeconds || 0) * 1000
    });
    this.setAssetAccessActive(Boolean(token));
    this.safeStorageRemove(CONSUMER_LOGOUT_PENDING_KEY);
    if (user) {
      this.writeCachedUser(user);
    }

  }

  private rememberTokenEpoch(token: string): void {
    if (!token) return;
    this.tokenEpochs.set(token, this.sessionEpoch);
    while (this.tokenEpochs.size > 16) {
      const oldest = this.tokenEpochs.keys().next().value as string | undefined;
      if (!oldest) break;
      this.tokenEpochs.delete(oldest);
    }
  }

  private setSnapshot(next: ConsumerSessionSnapshot): void {
    this.snapshot = next;
    for (const listener of this.listeners) listener(this.snapshot);
  }

  private deleteLegacyAccessToken(): void {
    this.safeStorageRemove(TOKEN_KEY);
  }

  private readCachedUser(): SessionUser | null {
    return parseStoredUser(this.safeStorageGet(USER_KEY));
  }

  private writeCachedUser(user: SessionUser | null): void {
    if (!user) {
      this.safeStorageRemove(USER_KEY);
      return;
    }
    this.safeStorageSet(USER_KEY, JSON.stringify(user));
  }

  private safeStorageGet(key: string): string | null {
    try {
      return this.storage?.getItem(key) ?? null;
    } catch {
      return null;
    }
  }

  private safeStorageSet(key: string, value: string): void {
    try {
      this.storage?.setItem(key, value);
    } catch {
      // In-memory authentication remains authoritative when storage is unavailable.
    }
  }

  private safeStorageRemove(key: string): void {
    try {
      this.storage?.removeItem(key);
    } catch {
      // In-memory authentication remains authoritative when storage is unavailable.
    }
  }

  private installCrossTabListeners(): void {
    this.windowValue?.addEventListener("storage", this.handleStorageEvent);
    this.broadcastChannel = this.createBroadcastChannel(CONSUMER_SESSION_BROADCAST_CHANNEL);
    if (!this.broadcastChannel) return;
    if (this.broadcastChannel.addEventListener) {
      this.broadcastChannel.addEventListener("message", this.handleChannelMessage);
    } else {
      this.broadcastChannel.onmessage = this.handleChannelMessage;
    }
  }

  private broadcastLogout(): void {
    const signal: LogoutSignal = { type: "logout", at: this.now() };
    this.broadcastChannel?.postMessage(signal);
    try {
      this.storage?.setItem(CONSUMER_SESSION_SIGNAL_KEY, JSON.stringify(signal));
    } catch {
      // Ignore quota or privacy-mode storage failures; local logout already succeeded.
    }
  }

  private setAssetAccessActive(active: boolean): void {
    const documentValue = this.windowValue?.document;
    if (!documentValue) return;
    const secure = this.windowValue?.location.protocol === "https:" ? "; Secure" : "";
    try {
      documentValue.cookie = active
        ? `${ASSET_ACTIVE_COOKIE_NAME}=1; Path=/uploads; SameSite=Strict${secure}`
        : `${ASSET_ACTIVE_COOKIE_NAME}=; Max-Age=0; Path=/uploads; SameSite=Strict${secure}`;
    } catch {
      // Session state must still transition when cookie storage is unavailable.
    }
  }

  getAdminStorage(): Storage | null {
    return this.adminStorage;
  }
}

function defaultBroadcastChannelFactory(name: string): BroadcastChannelLike | null {
  if (typeof BroadcastChannel === "undefined") return null;
  return new BroadcastChannel(name) as BroadcastChannelLike;
}

function defaultCredentialLock<T>(operation: () => Promise<T>): Promise<T> {
  if (typeof navigator === "undefined" || !navigator.locks) {
    return operation();
  }
  return navigator.locks.request(CONSUMER_CREDENTIAL_LOCK, operation);
}

function parseLogoutSignal(raw: string): LogoutSignal | null {
  try {
    const parsed: unknown = JSON.parse(raw);
    return isLogoutSignal(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

function isLogoutSignal(value: unknown): value is LogoutSignal {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Partial<LogoutSignal>;
  return candidate.type === "logout" && typeof candidate.at === "number" && Number.isFinite(candidate.at);
}

function storageOrNull(factory: () => Storage): Storage | null {
  try {
    return factory();
  } catch {
    return null;
  }
}

function windowOrNull(): Window | null {
  try {
    return window;
  } catch {
    return null;
  }
}

function waitForRefreshWinner(attempt: number): Promise<void> {
  const delay = 100 * 2 ** Math.min(attempt, 2);
  return new Promise((resolve) => globalThis.setTimeout(resolve, delay));
}

export function isAuthInvalidatingError(error: unknown): boolean {
  return error instanceof ApiError && (
    isInvalidRefreshSessionError(error) ||
    isRefreshSessionReplayedError(error)
  );
}
