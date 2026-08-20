const operationKeys = new Map<string, string>();

export function createChatOperationKey(
  operation: string,
  identity: string
): string {
  const normalized = `${operation}:${identity}`.slice(0, 180);
  const existing = operationKeys.get(normalized);
  if (existing) return existing;
  const random = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  const key = `web-chat:${operation}:${random}`.slice(0, 128);
  operationKeys.set(normalized, key);
  return key;
}

export function rotateChatOperationKey(operation: string, identity: string): void {
  operationKeys.delete(`${operation}:${identity}`.slice(0, 180));
}
