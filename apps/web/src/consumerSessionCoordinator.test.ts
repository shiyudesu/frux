// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  apiRequest,
  configureConsumerAuthController
} from "./api/client";
import {
  CONSUMER_LOGOUT_PENDING_KEY,
  CONSUMER_SESSION_SIGNAL_KEY,
  ConsumerSessionCoordinator
} from "./consumerSessionCoordinator";
import { ADMIN_SESSION_KEY } from "./admin/adminSession";
import { ASSET_ACTIVE_COOKIE_NAME, TOKEN_KEY, USER_KEY, emptyProfile } from "./constants";

interface BroadcastSubscriber {
  (event: MessageEvent<unknown>): void;
}

class FakeBroadcastChannel {
  static channels = new Map<string, Set<FakeBroadcastChannel>>();
  onmessage: ((event: MessageEvent<unknown>) => void) | null = null;
  private readonly listeners = new Set<BroadcastSubscriber>();

  constructor(private readonly name: string) {
    if (!FakeBroadcastChannel.channels.has(name)) {
      FakeBroadcastChannel.channels.set(name, new Set());
    }

    FakeBroadcastChannel.channels.get(name)!.add(this);
  }

  addEventListener(_type: "message", listener: BroadcastSubscriber) {
    this.listeners.add(listener);
  }

  removeEventListener(_type: "message", listener: BroadcastSubscriber) {
    this.listeners.delete(listener);
  }

  postMessage(message: unknown) {
    const peers = FakeBroadcastChannel.channels.get(this.name) || new Set();
    for (const peer of peers) {
      if (peer === this) continue;
      const event = { data: message } as MessageEvent<unknown>;
      peer.onmessage?.(event);
      for (const listener of peer.listeners) listener(event);
    }
  }

  close() {
    FakeBroadcastChannel.channels.get(this.name)?.delete(this);
  }

  static reset() {
    FakeBroadcastChannel.channels.clear();
  }
}

class FakeCredentialLock {
  static tail: Promise<void> = Promise.resolve();

  static async run<T>(operation: () => Promise<T>): Promise<T> {
    let release!: () => void;
    const previous = FakeCredentialLock.tail;
    FakeCredentialLock.tail = new Promise<void>((resolve) => {
      release = resolve;
    });
    await previous;
    try {
      return await operation();
    } finally {
      release();
    }

  }

  static reset() {
    FakeCredentialLock.tail = Promise.resolve();
  }
}

const throwingStorage = {
  get length(): number {
    throw new DOMException("storage unavailable", "SecurityError");
  },
  clear() {
    throw new DOMException("storage unavailable", "SecurityError");
  },
  getItem(): string | null {
    throw new DOMException("storage unavailable", "SecurityError");
  },
  key(): string | null {
    throw new DOMException("storage unavailable", "SecurityError");
  },
  removeItem() {
    throw new DOMException("storage unavailable", "SecurityError");
  },
  setItem() {
    throw new DOMException("storage unavailable", "QuotaExceededError");
  }
} satisfies Storage;

describe("consumer session coordinator", () => {
  beforeEach(() => {
    window.history.replaceState({}, "", "/uploads");
    localStorage.clear();
    sessionStorage.clear();
    vi.restoreAllMocks();
    FakeBroadcastChannel.reset();
    FakeCredentialLock.reset();
    document.cookie = `${ASSET_ACTIVE_COOKIE_NAME}=; Max-Age=0; Path=/uploads; SameSite=Strict`;
  });

  afterEach(() => {
    configureConsumerAuthController(null);
    localStorage.clear();
    sessionStorage.clear();
    FakeBroadcastChannel.reset();
    FakeCredentialLock.reset();
  });

  it("bootstraps from refresh, removes the legacy token key, and keeps access tokens out of storage", async () => {
    localStorage.setItem(TOKEN_KEY, "legacy-token");
    localStorage.setItem(USER_KEY, JSON.stringify({ ...emptyProfile, id: 9, account: "cached", nickname: "缓存用户" }));
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") {
        return jsonResponse({
          access_token: "fresh-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        expect((init?.headers as Record<string, string>).Authorization).toBe("Bearer fresh-token");
        return jsonResponse({ ...emptyProfile, id: 9, account: "fresh", nickname: "新用户" });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    await coordinator.bootstrap();

    expect(coordinator.getSnapshot()).toMatchObject({
      token: "fresh-token",
      status: "authenticated",
      user: expect.objectContaining({ nickname: "新用户" })
    });
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(localStorage.getItem(USER_KEY)).toContain("新用户");
    expect(sessionStorage.getItem(ADMIN_SESSION_KEY)).toBeNull();
    expect(document.cookie).toContain(`${ASSET_ACTIVE_COOKIE_NAME}=1`);
    coordinator.dispose();
  });

  it("keeps logout suppressed and retries durable revocation after an offline failure", async () => {
    localStorage.setItem(CONSUMER_LOGOUT_PENDING_KEY, "1");
    const fetchMock = vi.fn().mockRejectedValue(new TypeError("offline"));
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    await coordinator.bootstrap();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe("/api/sessions/current");
    expect(coordinator.getSnapshot()).toMatchObject({
      token: "",
      user: null,
      status: "unauthenticated"
    });

    coordinator.setAuth(
      "new-login-token",
      { ...emptyProfile, id: 7, account: "owner", nickname: "Owner" },
      300
    );
    expect(localStorage.getItem(CONSUMER_LOGOUT_PENDING_KEY)).toBeNull();
    coordinator.dispose();
  });

  it("clears the pending logout marker after bootstrap revocation succeeds", async () => {
    localStorage.setItem(CONSUMER_LOGOUT_PENDING_KEY, "1");
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, {
      status: 204
    }));
    vi.stubGlobal("fetch", fetchMock);
    const coordinator = createCoordinator();
    await coordinator.bootstrap();
    expect(localStorage.getItem(CONSUMER_LOGOUT_PENDING_KEY)).toBeNull();
    expect(coordinator.getSnapshot().status).toBe("unauthenticated");
    coordinator.dispose();
  });

  it("does not clear the shared asset marker on temporary bootstrap failure", async () => {
    document.cookie = `${ASSET_ACTIVE_COOKIE_NAME}=1; Path=/uploads; SameSite=Strict`;
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("offline")));
    const coordinator = createCoordinator();
    await coordinator.bootstrap();
    expect(coordinator.getSnapshot().status).toBe("unauthenticated");
    expect(document.cookie).toContain(`${ASSET_ACTIVE_COOKIE_NAME}=1`);
    coordinator.dispose();
  });

  it("does not let an in-flight refresh resurrect a locally suppressed logout", async () => {
    let resolveRefresh!: (response: Response) => void;
    const refreshResponse = new Promise<Response>((resolve) => {
      resolveRefresh = resolve;
    });
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") return refreshResponse;
      if (path === "/api/users/me") {
        return jsonResponse({ ...emptyProfile, id: 9, account: "owner", nickname: "Owner" });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    const refreshing = coordinator.refreshAccessToken();
    coordinator.beginLogout();
    resolveRefresh(jsonResponse({
      access_token: "stale-refresh-token",
      token_type: "Bearer",
      expires_in_seconds: 300
    }));

    await expect(refreshing).resolves.toBeNull();
    expect(coordinator.getSnapshot()).toMatchObject({
      token: "",
      user: null,
      status: "unauthenticated"
    });
    expect(localStorage.getItem(CONSUMER_LOGOUT_PENDING_KEY)).toBe("1");
    expect(fetchMock.mock.calls.some(([path]) => String(path) === "/api/users/me")).toBe(false);
    coordinator.dispose();
  });

  it("does not let bootstrap overwrite a newer explicit login", async () => {
    let resolveRefresh!: (response: Response) => void;
    const refreshResponse = new Promise<Response>((resolve) => {
      resolveRefresh = resolve;
    });
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") return refreshResponse;
      if (path === "/api/users/me") {
        return jsonResponse({ ...emptyProfile, id: 1, account: "stale", nickname: "Stale" });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    const bootstrapping = coordinator.bootstrap();
    coordinator.setAuth(
      "new-login-token",
      { ...emptyProfile, id: 2, account: "new", nickname: "New" },
      300
    );
    resolveRefresh(jsonResponse({
      access_token: "stale-bootstrap-token",
      token_type: "Bearer",
      expires_in_seconds: 300
    }));
    await bootstrapping;

    expect(coordinator.getSnapshot()).toMatchObject({
      token: "new-login-token",
      user: expect.objectContaining({ account: "new" }),
      status: "authenticated"
    });
    expect(fetchMock.mock.calls.some(([path]) => String(path) === "/api/users/me")).toBe(false);
    coordinator.dispose();
  });

  it("waits for cookie-mutating bootstrap before explicit credential mutation", async () => {
    let resolveRefresh!: (response: Response) => void;
    const refreshResponse = new Promise<Response>((resolve) => {
      resolveRefresh = resolve;
    });
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") return refreshResponse;
      if (path === "/api/users/me") {
        return jsonResponse({ ...emptyProfile, id: 1, account: "old", nickname: "Old" });
      }
      throw new Error(`unexpected request: ${path}`);
    }));

    const coordinator = createCoordinator();
    const bootstrap = coordinator.bootstrap();
    let prepared = false;
    const preparation = coordinator.runCredentialMutation(async () => {
      prepared = true;
    });
    await Promise.resolve();
    expect(prepared).toBe(false);
    resolveRefresh(jsonResponse({
      access_token: "bootstrap-token",
      token_type: "Bearer",
      expires_in_seconds: 300
    }));
    await bootstrap;
    await preparation;
    expect(prepared).toBe(true);
    coordinator.dispose();
  });

  it("serializes credential-cookie mutations across tabs", async () => {
    let releaseMutation!: () => void;
    const mutationRelease = new Promise<void>((resolve) => {
      releaseMutation = resolve;
    });
    let mutationEntered!: () => void;
    const entered = new Promise<void>((resolve) => {
      mutationEntered = resolve;
    });
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") {
        return errorResponse(401, "AUTH_REFRESH_INVALID", "invalid refresh");
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const first = createCoordinator();
    const second = createCoordinator();
    const mutation = first.runCredentialMutation(async () => {
      mutationEntered();
      await mutationRelease;
    });
    await entered;
    const refresh = second.refreshAccessToken();
    await Promise.resolve();
    expect(fetchMock).not.toHaveBeenCalled();
    releaseMutation();
    await mutation;
    await expect(refresh).resolves.toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    first.dispose();
    second.dispose();
  });

  it("keeps logout authoritative when queued behind an in-progress login", async () => {
    let releaseLogin!: () => void;
    const loginRelease = new Promise<void>((resolve) => {
      releaseLogin = resolve;
    });
    let loginEntered!: () => void;
    const entered = new Promise<void>((resolve) => {
      loginEntered = resolve;
    });
    const coordinator = createCoordinator();
    const loginMutation = coordinator.runCredentialMutation(async () => {
      loginEntered();
      await loginRelease;
      coordinator.setAuth(
        "login-token",
        { ...emptyProfile, id: 7, account: "owner", nickname: "Owner" },
        300
      );
    });
    await entered;
    coordinator.beginLogout();
    const logoutMutation = coordinator.runCredentialMutation(async () => {
      coordinator.clearAuth();
      coordinator.completeLogout();
    });
    releaseLogin();
    await loginMutation;
    await logoutMutation;
    expect(coordinator.getSnapshot()).toMatchObject({
      token: "",
      user: null,
      status: "unauthenticated"
    });
    coordinator.dispose();
  });

  it("cancels an authenticated mutation when preflight refresh changes account", async () => {
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") {
        return jsonResponse({
          access_token: "other-account-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({
          ...emptyProfile, id: 2, account: "other", nickname: "Other"
        });
      }
      throw new Error(`unexpected request: ${path}`);
    }));
    const coordinator = createCoordinator();
    coordinator.setAuth(
      "expiring-token",
      { ...emptyProfile, id: 1, account: "owner", nickname: "Owner" },
      1
    );
    const operation = vi.fn(async () => undefined);

    await expect(
      coordinator.runCredentialMutation(operation, true)
    ).rejects.toMatchObject({ code: "AUTH_SESSION_CHANGED" });
    expect(operation).not.toHaveBeenCalled();
    expect(coordinator.getSnapshot().user?.account).toBe("other");
    coordinator.dispose();
  });

  it("shares one refresh across concurrent authenticated requests", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      const token = readAuthorization(init);
      if (path === "/api/protected") {
        if (token === "expired-token") {
          return errorResponse(401, "AUTH_INVALID_ACCESS_TOKEN", "invalid access token");
        }
        return jsonResponse({ ok: true, token });
      }
      if (path === "/api/sessions/current/refresh") {
        return jsonResponse({
          access_token: "fresh-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({ ...emptyProfile, id: 5, account: "owner", nickname: "Owner" });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth("expired-token", { ...emptyProfile, id: 5, account: "owner", nickname: "Owner" }, 300);

    const [first, second] = await Promise.all([
      apiRequest<{ ok: boolean; token: string }>("/api/protected", { auth: "consumer", token: "expired-token" }),
      apiRequest<{ ok: boolean; token: string }>("/api/protected", { auth: "consumer", token: "expired-token" })
    ]);

    expect(first.token).toBe("fresh-token");
    expect(second.token).toBe("fresh-token");
    expect(fetchMock.mock.calls.filter(([path]) => String(path) === "/api/sessions/current/refresh")).toHaveLength(1);
    expect(fetchMock.mock.calls.filter(([path]) => String(path) === "/api/users/me")).toHaveLength(1);
    coordinator.dispose();
  });

  it("adopts same-account profile updates after transparent token refresh", async () => {
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") {
        return jsonResponse({
          access_token: "refreshed-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({
          ...emptyProfile, id: 5, account: "owner", nickname: "Owner"
        });
      }
      throw new Error(`unexpected request: ${path}`);
    }));
    const coordinator = createCoordinator();
    coordinator.setAuth(
      "old-token",
      { ...emptyProfile, id: 5, account: "owner", nickname: "Before" },
      300
    );
    await coordinator.refreshAccessToken();
    coordinator.updateUser(
      "old-token",
      { ...emptyProfile, id: 5, account: "owner", nickname: "After" }
    );
    expect(coordinator.getSnapshot().user?.nickname).toBe("After");

    coordinator.setAuth(
      "other-token",
      { ...emptyProfile, id: 6, account: "other", nickname: "Other" },
      300
    );
    coordinator.updateUser(
      "old-token",
      { ...emptyProfile, id: 5, account: "owner", nickname: "Stale" }
    );
    expect(coordinator.getSnapshot().user?.account).toBe("other");
    coordinator.dispose();
  });

  it("retries an authenticated request at most once", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      const token = readAuthorization(init);
      if (path === "/api/protected") {
        if (token === "expired-token" || token === "fresh-token") {
          return errorResponse(401, "AUTH_INVALID_ACCESS_TOKEN", "invalid access token");
        }
      }
      if (path === "/api/sessions/current/refresh") {
        return jsonResponse({
          access_token: "fresh-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({ ...emptyProfile, id: 5, account: "owner", nickname: "Owner" });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth("expired-token", { ...emptyProfile, id: 5, account: "owner", nickname: "Owner" }, 300);

    await expect(apiRequest("/api/protected", { auth: "consumer", token: "expired-token" }))
      .rejects.toMatchObject({ code: "AUTH_INVALID_ACCESS_TOKEN" });
    expect(fetchMock.mock.calls.filter(([path]) => String(path) === "/api/sessions/current/refresh")).toHaveLength(1);
    expect(fetchMock.mock.calls.filter(([path]) => String(path) === "/api/protected")).toHaveLength(2);
    coordinator.dispose();
  });

  it("does not replay an old account request after an explicit session switch", async () => {
    let resolveProtected!: (response: Response) => void;
    const protectedResponse = new Promise<Response>((resolve) => {
      resolveProtected = resolve;
    });
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/protected") return protectedResponse;
      if (path === "/api/sessions/current/refresh") {
        throw new Error("old account request must not refresh");
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth(
      "old-account-token",
      { ...emptyProfile, id: 1, account: "old", nickname: "Old" },
      300
    );
    const request = apiRequest("/api/protected", { auth: "consumer" });
    coordinator.setAuth(
      "new-account-token",
      { ...emptyProfile, id: 2, account: "new", nickname: "New" },
      300
    );
    resolveProtected(errorResponse(
      401, "AUTH_INVALID_ACCESS_TOKEN", "invalid access token",
    ));

    await expect(request).rejects.toMatchObject({
      code: "AUTH_SESSION_CHANGED"
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    coordinator.dispose();
  });

  it("rejects a successful response that completes after an account switch", async () => {
    let resolveProtected!: (response: Response) => void;
    const protectedResponse = new Promise<Response>((resolve) => {
      resolveProtected = resolve;
    });
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === "/api/protected") return protectedResponse;
      throw new Error(`unexpected request: ${String(input)}`);
    }));
    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth(
      "old-token",
      { ...emptyProfile, id: 1, account: "old", nickname: "Old" },
      300
    );
    const request = apiRequest("/api/protected", { auth: "consumer" });
    coordinator.setAuth(
      "new-token",
      { ...emptyProfile, id: 2, account: "new", nickname: "New" },
      300
    );
    resolveProtected(jsonResponse({ private: "old-account-data" }));

    await expect(request).rejects.toMatchObject({
      code: "AUTH_SESSION_CHANGED"
    });
    coordinator.dispose();
  });

  it("rejects a stale callback before it sends under a replacement account", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth(
      "old-token",
      { ...emptyProfile, id: 1, account: "old", nickname: "Old" },
      300
    );
    coordinator.setAuth(
      "new-token",
      { ...emptyProfile, id: 2, account: "new", nickname: "New" },
      300
    );

    await expect(
      apiRequest("/api/protected", {
        auth: "consumer",
        token: "old-token"
      })
    ).rejects.toMatchObject({ code: "AUTH_SESSION_CHANGED" });
    expect(fetchMock).not.toHaveBeenCalled();
    coordinator.dispose();
  });

  it("allows a same-account stale token to adopt the refreshed token", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") {
        return jsonResponse({
          access_token: "refreshed-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({
          ...emptyProfile, id: 1, account: "owner", nickname: "Owner"
        });
      }
      if (path === "/api/protected") {
        return jsonResponse({ token: readAuthorization(init) });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth(
      "old-token",
      { ...emptyProfile, id: 1, account: "owner", nickname: "Owner" },
      300
    );
    await coordinator.refreshAccessToken();

    await expect(
      apiRequest<{ token: string }>("/api/protected", {
        auth: "consumer",
        token: "old-token"
      })
    ).resolves.toEqual({ token: "refreshed-token" });
    coordinator.dispose();
  });

  it("does not send the request after preflight refresh invalidates the session", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") {
        return errorResponse(401, "AUTH_REFRESH_INVALID", "invalid refresh");
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth(
      "expiring-token",
      { ...emptyProfile, id: 1, account: "owner", nickname: "Owner" },
      1
    );

    await expect(
      apiRequest("/api/protected", { auth: "consumer" })
    ).rejects.toMatchObject({
      code: expect.stringMatching(/AUTH_(INVALID_ACCESS_TOKEN|SESSION_CHANGED)/)
    });
    expect(fetchMock.mock.calls.some(
      ([path]) => String(path) === "/api/protected"
    )).toBe(false);
    coordinator.dispose();
  });

  it("recovers AUTHENTICATION_REQUIRED when a bearer token was supplied", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/api/protected") {
        const token = readAuthorization(init);
        if (token === "old-token") {
          return errorResponse(
            401, "AUTHENTICATION_REQUIRED", "authentication required"
          );
        }
        return jsonResponse({ token });
      }
      if (path === "/api/sessions/current/refresh") {
        return jsonResponse({
          access_token: "new-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({
          ...emptyProfile, id: 1, account: "owner", nickname: "Owner"
        });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth(
      "old-token",
      { ...emptyProfile, id: 1, account: "owner", nickname: "Owner" },
      300
    );

    await expect(
      apiRequest<{ token: string }>("/api/protected", { auth: "consumer" })
    ).resolves.toEqual({ token: "new-token" });
    coordinator.dispose();
  });

  it("does not retry an old-account request when refresh reveals another account", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/protected") {
        return errorResponse(
          401, "AUTH_INVALID_ACCESS_TOKEN", "invalid access token"
        );
      }
      if (path === "/api/sessions/current/refresh") {
        return jsonResponse({
          access_token: "other-account-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({
          ...emptyProfile, id: 2, account: "other", nickname: "Other"
        });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth(
      "old-account-token",
      { ...emptyProfile, id: 1, account: "old", nickname: "Old" },
      300
    );
    await expect(
      apiRequest("/api/protected", { auth: "consumer" })
    ).rejects.toMatchObject({ code: "AUTH_SESSION_CHANGED" });
    expect(fetchMock.mock.calls.filter(
      ([path]) => String(path) === "/api/protected"
    )).toHaveLength(1);
    expect(coordinator.getSnapshot()).toMatchObject({
      token: "other-account-token",
      user: expect.objectContaining({ account: "other" })
    });
    coordinator.dispose();
  });

  it("retries a superseded refresh once and adopts the replacement token", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") {
        const attempt = fetchMock.mock.calls.filter(([candidate]) => String(candidate) === path).length;
        if (attempt === 1) {
          return errorResponse(409, "AUTH_REFRESH_SESSION_SUPERSEDED", "refresh superseded");
        }
        return jsonResponse({
          access_token: "replacement-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({ ...emptyProfile, id: 12, account: "owner", nickname: "Owner" });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    const token = await coordinator.refreshAccessToken();

    expect(token).toBe("replacement-token");
    expect(coordinator.getSnapshot().token).toBe("replacement-token");
    expect(fetchMock.mock.calls.filter(([path]) => String(path) === "/api/sessions/current/refresh")).toHaveLength(2);
    coordinator.dispose();
  });

  it("uses bounded backoff across repeated cross-tab superseded refreshes", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") {
        const attempt = fetchMock.mock.calls.filter(
          ([candidate]) => String(candidate) === path
        ).length;
        if (attempt <= 2) {
          return errorResponse(
            409, "AUTH_REFRESH_SUPERSEDED", "refresh superseded"
          );
        }
        return jsonResponse({
          access_token: "third-tab-token",
          token_type: "Bearer",
          expires_in_seconds: 300
        });
      }
      if (path === "/api/users/me") {
        return jsonResponse({
          ...emptyProfile, id: 13, account: "owner", nickname: "Owner"
        });
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    await expect(coordinator.refreshAccessToken()).resolves.toBe("third-tab-token");
    expect(fetchMock.mock.calls.filter(
      ([path]) => String(path) === "/api/sessions/current/refresh"
    )).toHaveLength(3);
    coordinator.dispose();
  });

  it("broadcasts consumer logout across tabs without touching admin session storage", async () => {
    sessionStorage.setItem(ADMIN_SESSION_KEY, "admin-session");
    const first = createCoordinator();
    const second = createCoordinator();
    first.setAuth("first-token", { ...emptyProfile, id: 1, account: "first", nickname: "First" }, 300);
    second.setAuth("second-token", { ...emptyProfile, id: 2, account: "second", nickname: "Second" }, 300);

    first.clearAuth();

    expect(first.getSnapshot().token).toBe("");
    expect(second.getSnapshot().token).toBe("");
    expect(sessionStorage.getItem(ADMIN_SESSION_KEY)).toBe("admin-session");
    expect(localStorage.getItem(CONSUMER_SESSION_SIGNAL_KEY)).toContain("logout");
    first.dispose();
    second.dispose();
  });

  it("can restart cross-tab listeners after a StrictMode-style cleanup", () => {
    const first = createCoordinator();
    const second = createCoordinator();
    second.setAuth(
      "second-token",
      { ...emptyProfile, id: 2, account: "second", nickname: "Second" },
      300
    );
    second.stop();
    first.clearAuth();
    expect(second.getSnapshot().token).toBe("second-token");

    second.start();
    first.setAuth(
      "first-token",
      { ...emptyProfile, id: 1, account: "first", nickname: "First" },
      300
    );
    first.clearAuth();
    expect(second.getSnapshot().token).toBe("");
    first.dispose();
    second.dispose();
  });

  it("keeps in-memory login and logout authoritative when storage throws", () => {
    const coordinator = new ConsumerSessionCoordinator({
      storage: throwingStorage,
      createBroadcastChannel: (name) => new FakeBroadcastChannel(name),
      runCredentialLock: (operation) => FakeCredentialLock.run(operation)
    });
    coordinator.start();
    expect(() => coordinator.setAuth(
      "token",
      { ...emptyProfile, id: 7, account: "owner", nickname: "Owner" },
      300
    )).not.toThrow();
    expect(coordinator.getSnapshot()).toMatchObject({
      token: "token",
      status: "authenticated"
    });
    expect(() => coordinator.beginLogout()).not.toThrow();
    expect(coordinator.getSnapshot()).toMatchObject({
      token: "",
      user: null,
      status: "unauthenticated"
    });
    coordinator.dispose();
  });

  it("invalidates an in-flight refresh when another tab completes logout", async () => {
    let resolveRefresh!: (response: Response) => void;
    const refreshResponse = new Promise<Response>((resolve) => {
      resolveRefresh = resolve;
    });
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/sessions/current/refresh") return refreshResponse;
      if (path === "/api/users/me") {
        return jsonResponse({ ...emptyProfile, id: 2, account: "second", nickname: "Second" });
      }
      throw new Error(`unexpected request: ${path}`);
    }));

    const first = createCoordinator();
    const second = createCoordinator();
    const refreshing = second.refreshAccessToken();
    first.beginLogout();
    first.completeLogout();
    resolveRefresh(jsonResponse({
      access_token: "stale-cross-tab-token",
      token_type: "Bearer",
      expires_in_seconds: 300
    }));

    await expect(refreshing).resolves.toBeNull();
    expect(second.getSnapshot()).toMatchObject({
      token: "",
      user: null,
      status: "unauthenticated"
    });
    first.dispose();
    second.dispose();
  });

  it("keeps the current session when refresh is temporarily unavailable", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      const token = readAuthorization(init);
      if (path === "/api/protected") {
        if (token === "expired-token") {
          return errorResponse(401, "AUTH_INVALID_ACCESS_TOKEN", "invalid access token");
        }
        return jsonResponse({ ok: true });
      }
      if (path === "/api/sessions/current/refresh") {
        return errorResponse(503, "AUTHENTICATION_UNAVAILABLE", "authentication unavailable");
      }
      throw new Error(`unexpected request: ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const coordinator = createCoordinator();
    configureConsumerAuthController(coordinator);
    coordinator.setAuth("expired-token", { ...emptyProfile, id: 8, account: "owner", nickname: "Owner" }, 300);

    await expect(apiRequest("/api/protected", { auth: "consumer", token: "expired-token" }))
      .rejects.toBeInstanceOf(ApiError);
    expect(coordinator.getSnapshot()).toMatchObject({
      token: "expired-token",
      status: "authenticated",
      user: expect.objectContaining({ nickname: "Owner" })
    });
    coordinator.dispose();
  });
});

function createCoordinator(): ConsumerSessionCoordinator {
  const coordinator = new ConsumerSessionCoordinator({
    createBroadcastChannel: (name) => new FakeBroadcastChannel(name),
    runCredentialLock: (operation) => FakeCredentialLock.run(operation)
  });
  coordinator.start();
  return coordinator;
}

function readAuthorization(init?: RequestInit): string {
  return ((init?.headers as Record<string, string> | undefined)?.Authorization || "")
    .replace(/^Bearer\s+/i, "");
}

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: init.status || 200,
    headers: {
      "Content-Type": "application/json",
      ...(init.headers || {})
    }
  });
}

function errorResponse(status: number, code: string, error: string): Response {
  return jsonResponse({ code, error }, { status });
}
