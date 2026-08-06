import type { AdminPrincipal } from "../types";
import { apiRequest } from "./client";

export function fetchAdminPrincipal(token: string): Promise<AdminPrincipal> {
  return apiRequest<AdminPrincipal>("/api/admin/me", { token });
}
