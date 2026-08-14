const PASSWORD_ENCODER = new TextEncoder();

export const PASSWORD_RULE_MESSAGE = "密码至少需要 8 个字符，且 UTF-8 编码不能超过 72 字节";

export function normalizeSelectedPassword(value: string): string {
  return value.trim();
}

export function passwordCodePointLength(value: string): number {
  return Array.from(normalizeSelectedPassword(value)).length;
}

export function passwordByteLength(value: string): number {
  return PASSWORD_ENCODER.encode(normalizeSelectedPassword(value)).length;
}

export function validateSelectedPassword(value: string): string | null {
  const normalized = normalizeSelectedPassword(value);
  if (Array.from(normalized).length < 8) return PASSWORD_RULE_MESSAGE;
  if (PASSWORD_ENCODER.encode(normalized).length > 72) return PASSWORD_RULE_MESSAGE;
  return null;
}
