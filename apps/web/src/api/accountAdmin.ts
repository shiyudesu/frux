import type {
  ManageAccountRequest,
  ManageAccountResponse,
  ManagedAccount,
  ManagedAccountPage,
  ManagedAccountSearchFilters
} from "../types";
import { apiRequest } from "./client";

export function managedAccountSearchPath(
  filters: ManagedAccountSearchFilters,
  cursor = "",
  limit = 20
): string {
  const params = new URLSearchParams();
  if (filters.query.trim()) params.set("query", filters.query.trim());
  if (filters.user_id.trim()) params.set("user_id", filters.user_id.trim());
  if (filters.status) params.set("status", filters.status);
  if (cursor) params.set("cursor", cursor);
  params.set("limit", String(limit));
  return `/api/admin/accounts?${params.toString()}`;
}

export function searchManagedAccounts(
  token: string,
  filters: ManagedAccountSearchFilters,
  cursor = "",
  limit = 20
): Promise<ManagedAccountPage> {
  return apiRequest<ManagedAccountPage>(
    managedAccountSearchPath(filters, cursor, limit),
    { token }
  );
}

export function fetchManagedAccount(
  token: string,
  userID: number
): Promise<ManagedAccount> {
  return apiRequest<ManagedAccount>(`/api/admin/accounts/${userID}`, { token });
}

export function freezeManagedAccount(
  token: string,
  userID: number,
  body: ManageAccountRequest,
  idempotencyKey: string
): Promise<ManageAccountResponse> {
  return manageAccount(token, userID, "freeze", body, idempotencyKey);
}

export function unfreezeManagedAccount(
  token: string,
  userID: number,
  body: ManageAccountRequest,
  idempotencyKey: string
): Promise<ManageAccountResponse> {
  return manageAccount(token, userID, "unfreeze", body, idempotencyKey);
}

export function revokeManagedAccountSessions(
  token: string,
  userID: number,
  body: ManageAccountRequest,
  idempotencyKey: string
): Promise<ManageAccountResponse> {
  return manageAccount(token, userID, "sessions/revoke", body, idempotencyKey);
}

function manageAccount(
  token: string,
  userID: number,
  operation: "freeze" | "unfreeze" | "sessions/revoke",
  body: ManageAccountRequest,
  idempotencyKey: string
): Promise<ManageAccountResponse> {
  return apiRequest<ManageAccountResponse>(
    `/api/admin/accounts/${userID}/${operation}`,
    {
      method: "POST",
      token,
      headers: { "Idempotency-Key": idempotencyKey },
      body
    }
  );
}
