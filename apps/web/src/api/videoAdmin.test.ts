import { describe, expect, it } from "vitest";
import { adminVideoSearchPath } from "./videoAdmin";
import type { AdminVideoSearchFilters } from "../types";

describe("admin video search cursor binding", () => {
  it("authors each page from the applied filter set", () => {
    const filters: AdminVideoSearchFilters = {
      status: "rejected",
      author_id: "7",
      video_id: "",
      keyword: "policy",
      created_from: "2026-08-01T00:00",
      created_to: "2026-08-06T00:00"
    };
    const path = adminVideoSearchPath(filters, "signed-cursor", 20);
    const url = new URL(path, "https://frux.test");
    expect(url.searchParams.get("status")).toBe("rejected");
    expect(url.searchParams.get("author_id")).toBe("7");
    expect(url.searchParams.get("keyword")).toBe("policy");
    expect(url.searchParams.get("cursor")).toBe("signed-cursor");
    expect(url.searchParams.get("created_from")).toBe(new Date(filters.created_from).toISOString());
    expect(url.searchParams.get("created_to")).toBe(new Date(filters.created_to).toISOString());
  });
});
