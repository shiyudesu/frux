import type { ReviewLeaseResponse } from "../types";

const leases = new Map<number, ReviewLeaseResponse>();

export function getReviewLease(reviewID: number): ReviewLeaseResponse | null {
  return leases.get(reviewID) ?? null;
}

export function rememberReviewLease(reviewID: number, lease: ReviewLeaseResponse): void {
  leases.set(reviewID, lease);
}

export function forgetReviewLease(reviewID: number): void {
  leases.delete(reviewID);
}
