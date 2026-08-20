export interface CreatorArchiveYear {
  year: string;
  months: string[];
}

const CREATOR_ARCHIVE_MONTH_PATTERN = /^(\d{4})-(0[1-9]|1[0-2])$/;

export function isCreatorArchiveMonth(value: unknown): value is string {
  return typeof value === "string" && CREATOR_ARCHIVE_MONTH_PATTERN.test(value);
}

export function formatCreatorArchiveMonth(value: string): string {
  const match = CREATOR_ARCHIVE_MONTH_PATTERN.exec(value);
  if (!match) return "日期筛选";
  return `${match[1]}年${Number(match[2])}月`;
}

export function groupCreatorArchiveMonths(values: readonly string[]): CreatorArchiveYear[] {
  const unique = [...new Set(values.filter(isCreatorArchiveMonth))].sort((left, right) =>
    right.localeCompare(left)
  );
  const groups: CreatorArchiveYear[] = [];
  for (const value of unique) {
    const [year] = value.split("-");
    const current = groups[groups.length - 1];
    if (!current || current.year !== year) {
      groups.push({ year, months: [value] });
    } else {
      current.months.push(value);
    }
  }
  return groups;
}

export function creatorArchiveMonthRange(value: string): {
  createdFrom: string;
  createdTo: string;
} {
  const match = CREATOR_ARCHIVE_MONTH_PATTERN.exec(value);
  if (!match) return { createdFrom: "", createdTo: "" };
  const year = Number(match[1]);
  const month = Number(match[2]);
  const lastDay = new Date(Date.UTC(year, month, 0)).getUTCDate();
  return {
    createdFrom: `${match[1]}-${match[2]}-01`,
    createdTo: `${match[1]}-${match[2]}-${String(lastDay).padStart(2, "0")}`
  };
}
