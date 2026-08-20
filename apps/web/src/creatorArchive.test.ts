import { describe, expect, it } from "vitest";
import {
  creatorArchiveMonthRange,
  formatCreatorArchiveMonth,
  groupCreatorArchiveMonths,
  isCreatorArchiveMonth
} from "./creatorArchive";

describe("creator archive month helpers", () => {
  it("validates and formats canonical months", () => {
    expect(isCreatorArchiveMonth("2026-08")).toBe(true);
    expect(isCreatorArchiveMonth("2026-8")).toBe(false);
    expect(isCreatorArchiveMonth("2026-13")).toBe(false);
    expect(formatCreatorArchiveMonth("2026-08")).toBe("2026年8月");
    expect(formatCreatorArchiveMonth("invalid")).toBe("日期筛选");
  });

  it("groups unique months by descending year and month", () => {
    expect(groupCreatorArchiveMonths([
      "2025-12",
      "2026-02",
      "invalid",
      "2026-08",
      "2026-08"
    ])).toEqual([
      { year: "2026", months: ["2026-08", "2026-02"] },
      { year: "2025", months: ["2025-12"] }
    ]);
  });

  it("maps months to inclusive UTC date-only ranges", () => {
    expect(creatorArchiveMonthRange("2026-02")).toEqual({
      createdFrom: "2026-02-01",
      createdTo: "2026-02-28"
    });
    expect(creatorArchiveMonthRange("2024-02")).toEqual({
      createdFrom: "2024-02-01",
      createdTo: "2024-02-29"
    });
    expect(creatorArchiveMonthRange("")).toEqual({ createdFrom: "", createdTo: "" });
  });
});
