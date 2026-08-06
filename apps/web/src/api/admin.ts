import type { AdminLoginResponse, AdminPrincipal } from "../types";
import { apiRequest } from "./client";

export function loginAdmin(account: string, password: string): Promise<AdminLoginResponse> {
  return apiRequest<AdminLoginResponse>("/api/admin/auth/login", {
    method: "POST",
    body: { account, password }
  });
}

export function fetchAdminPrincipal(token: string): Promise<AdminPrincipal> {
  return apiRequest<AdminPrincipal>("/api/admin/me", { token });
}
