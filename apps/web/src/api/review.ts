import type {
  ReviewCase,
  ReviewCaseDetail,
  ReviewDecisionResponse,
  ReviewLeaseResponse,
  ReviewPreviewAccess,
  ReviewQueueScope,
  ReviewQueuePage
} from "../types";
import { apiRequest } from "./client";

export interface ReviewQueueQuery {
  scope?: ReviewQueueScope;
  minPriority?: number;
  maxPriority?: number;
  cursor?: string;
  limit?: number;
}

export function fetchReviewQueue(token: string, query: ReviewQueueQuery = {}): Promise<ReviewQueuePage> {
  const params = new URLSearchParams();
  if (query.scope) params.set("scope", query.scope);
  if (query.minPriority !== undefined) params.set("min_priority", String(query.minPriority));
  if (query.maxPriority !== undefined) params.set("max_priority", String(query.maxPriority));
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.limit) params.set("limit", String(query.limit));
  const search = params.toString();
  return apiRequest<ReviewQueuePage>(`/api/admin/review/cases${search ? `?${search}` : ""}`, { token });
}

export function fetchReviewPreview(token: string, reviewID: number): Promise<ReviewPreviewAccess> {
  return apiRequest<ReviewPreviewAccess>(
    `/api/admin/review/cases/${reviewID}/preview-access`,
    { token, cache: "no-store" }
  );
}

export function fetchReviewCase(token: string, reviewID: number): Promise<ReviewCaseDetail> {
  return apiRequest<ReviewCaseDetail>(`/api/admin/review/cases/${reviewID}`, { token });
}

export function claimReviewCase(
  token: string,
  reviewID: number,
  expectedCaseVersion: number
): Promise<ReviewLeaseResponse> {
  return apiRequest<ReviewLeaseResponse>(`/api/admin/review/cases/${reviewID}/claim`, {
    method: "POST",
    token,
    body: { expected_case_version: expectedCaseVersion }
  });
}

export function renewReviewLease(
  token: string,
  reviewID: number,
  leaseToken: string,
  expectedCaseVersion: number
): Promise<ReviewLeaseResponse> {
  return apiRequest<ReviewLeaseResponse>(`/api/admin/review/cases/${reviewID}/lease/renew`, {
    method: "POST",
    token,
    body: { lease_token: leaseToken, expected_case_version: expectedCaseVersion }
  });
}

export function resumeReviewLease(
  token: string,
  reviewID: number,
  expectedCaseVersion: number
): Promise<ReviewLeaseResponse> {
  return apiRequest<ReviewLeaseResponse>(`/api/admin/review/cases/${reviewID}/lease/resume`, {
    method: "POST",
    token,
    body: { expected_case_version: expectedCaseVersion }
  });
}

export function releaseReviewLease(
  token: string,
  reviewID: number,
  leaseToken: string,
  expectedCaseVersion: number
): Promise<ReviewCase> {
  return apiRequest<ReviewCase>(`/api/admin/review/cases/${reviewID}/lease`, {
    method: "DELETE",
    token,
    body: { lease_token: leaseToken, expected_case_version: expectedCaseVersion }
  });
}

export function decideReviewCase(
  token: string,
  reviewID: number,
  input: {
    leaseToken: string;
    expectedCaseVersion: number;
    reviewVersion: number;
    outcome: "approve" | "reject";
    reasonCode: string;
    note: string;
    idempotencyKey: string;
  }
): Promise<ReviewDecisionResponse> {
  return apiRequest<ReviewDecisionResponse>(`/api/admin/review/cases/${reviewID}/decision`, {
    method: "POST",
    token,
    headers: { "Idempotency-Key": input.idempotencyKey },
    body: {
      lease_token: input.leaseToken,
      expected_case_version: input.expectedCaseVersion,
      review_version: input.reviewVersion,
      outcome: input.outcome,
      reason_code: input.reasonCode,
      note: input.note
    }
  });
}
