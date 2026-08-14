// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import {
  configureConsumerAuthController,
  uploadFile
} from "./client";

afterEach(() => {
  configureConsumerAuthController(null);
  vi.unstubAllGlobals();
});

it("refreshes an expired avatar upload token and retries once", async () => {
  configureConsumerAuthController({
    getAccessToken: () => "expired-token",
    getAccessExpiresAt: () => Date.now() + 300_000,
    getSessionEpoch: () => 1,
    getTokenEpoch: () => 1,
    refreshAccessToken: () => Promise.resolve("refreshed-token")
  });
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({
      code: "AUTH_INVALID_ACCESS_TOKEN",
      error: "invalid access token"
    }), {
      status: 401,
      headers: { "Content-Type": "application/json" }
    }))
    .mockResolvedValueOnce(new Response(JSON.stringify({
      url: "/uploads/avatar/file.png",
      kind: "avatar",
      filename: "file.png",
      size: 4
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
  vi.stubGlobal("fetch", fetchMock);

  await expect(
    uploadFile(new File(["data"], "file.png", { type: "image/png" }), "avatar", "fallback-token")
  ).resolves.toMatchObject({ url: "/uploads/avatar/file.png" });

  const firstHeaders = fetchMock.mock.calls[0][1]?.headers as Record<string, string>;
  const secondHeaders = fetchMock.mock.calls[1][1]?.headers as Record<string, string>;
  expect(firstHeaders.Authorization).toBe("Bearer expired-token");
  expect(secondHeaders.Authorization).toBe("Bearer refreshed-token");
});
