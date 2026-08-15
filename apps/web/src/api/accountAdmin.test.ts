import { describe, expect, it } from "vitest";
import type { ManagedAccountSearchFilters } from "../types";
import { managedAccountSearchPath } from "./accountAdmin";

describe("managed account search path", () => {
  it("binds filters and cursor to the requested page", () => {
    const filters: ManagedAccountSearchFilters = {
      query: " alice ",
      user_id: "42",
      status: "frozen"
    };
    const path = managedAccountSearchPath(filters, "signed-cursor", 20);
    const url = new URL(path, "https://frux.test");
    expect(url.searchParams.get("query")).toBe("alice");
    expect(url.searchParams.get("user_id")).toBe("42");
    expect(url.searchParams.get("status")).toBe("frozen");
    expect(url.searchParams.get("cursor")).toBe("signed-cursor");
    expect(url.searchParams.get("limit")).toBe("20");
  });
});
