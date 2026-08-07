import { afterEach, describe, expect, it, vi } from "vitest";
import { resolveCreatorVideoTarget } from "./creator";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("creator lifecycle message target resolution", () => {
  it("resolves a private target across both visibility queries", async () => {
    const calls: Array<{ visibility: string; video_id: number }> = [];
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (_path, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body || "{}")) as {
        visibility: string;
        video_id: number;
      };
      calls.push({ visibility: body.visibility, video_id: body.video_id });
      const items = body.visibility === "private"
        ? [{
            id: 42, author_id: 7, title: "Private", description: "",
            media_url: "", cover_url: "", status: 2, visibility: "private",
            like_count: 0, comment_count: 0, favorite_count: 0,
            created_at: "2026-08-07T00:00:00Z", updated_at: "2026-08-07T00:00:00Z"
          }]
        : [];
      return new Response(JSON.stringify({
        items, next_cursor: "", has_more: false
      }), {
        status: 200,
        headers: { "Content-Type": "application/json" }
      });
    }));

    const target = await resolveCreatorVideoTarget("token", 42);
    expect(target?.tab).toBe("private");
    expect(target?.video.id).toBe(42);
    expect(calls).toEqual([
      { visibility: "public", video_id: 42 },
      { visibility: "private", video_id: 42 }
    ]);
  });

  it("returns unavailable when neither visibility owns the target", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async () => new Response(JSON.stringify({
      items: [], next_cursor: "", has_more: false
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    })));
    await expect(resolveCreatorVideoTarget("token", 99)).resolves.toBeNull();
  });
});
