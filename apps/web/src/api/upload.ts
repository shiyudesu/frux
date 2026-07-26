import type {
  CompleteUploadSessionResponse,
  UploadResponse,
  UploadSessionRequest,
  UploadSessionResponse
} from "../types";
import { ApiError, apiRequest } from "./client";

export type UploadKind = "video" | "cover";

export type MediaUploadResult =
  | { mode: "direct"; assetID: number }
  | { mode: "multipart"; url: string };

export async function uploadMediaFile(
  file: File,
  kind: UploadKind,
  token: string,
  attemptID: string,
  onProgress?: (progress: number) => void
): Promise<MediaUploadResult> {
  const checksum = await sha256File(file);
  const request: UploadSessionRequest = {
    kind,
    filename: file.name,
    content_type: file.type || defaultContentType(kind),
    size_bytes: file.size,
    checksum_sha256: checksum
  };
  const idempotencyKey = uploadIdempotencyKey(attemptID, kind);
  const session = await apiRequest<UploadSessionResponse>("/api/upload-sessions", {
    method: "POST",
    token,
    headers: { "Idempotency-Key": idempotencyKey },
    body: request
  });
  if (session.mode === "multipart") {
    const uploaded = await uploadMultipart(file, kind, token, onProgress);
    return { mode: "multipart", url: uploaded.url };
  }
  if (session.completed_asset_id) {
    onProgress?.(100);
    return { mode: "direct", assetID: session.completed_asset_id };
  }
  if (!session.id || !session.upload) {
    throw new Error("上传会话响应不完整");
  }
  await uploadDirect(file, session.upload.url, session.upload.method, session.upload.headers, onProgress);
  const completed = await apiRequest<CompleteUploadSessionResponse>(
    `/api/upload-sessions/${encodeURIComponent(session.id)}/complete`,
    { method: "POST", token }
  );
  return { mode: "direct", assetID: completed.asset.id };
}

async function sha256File(file: File): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
  return Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join("");
}

function uploadDirect(
  file: File,
  url: string,
  method: string,
  headers: Record<string, string>,
  onProgress?: (progress: number) => void
): Promise<void> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open(method || "PUT", url);
    for (const [name, value] of Object.entries(headers)) {
      request.setRequestHeader(name, value);
    }
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress?.(Math.round((event.loaded / event.total) * 100));
    };
    request.onerror = () => reject(new Error("对象存储上传失败"));
    request.onload = () => {
      if (request.status >= 200 && request.status < 300) {
        onProgress?.(100);
        resolve();
        return;
      }
      reject(new Error(`对象存储上传失败 (${request.status})`));
    };
    request.send(file);
  });
}

function uploadMultipart(
  file: File,
  kind: UploadKind,
  token: string,
  onProgress?: (progress: number) => void
): Promise<UploadResponse> {
  return new Promise((resolve, reject) => {
    const body = new FormData();
    body.append("file", file);
    body.append("kind", kind);
    const request = new XMLHttpRequest();
    request.open("POST", "/api/uploads");
    request.setRequestHeader("Accept", "application/json");
    request.setRequestHeader("Authorization", ["Bearer", token].join(" "));
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress?.(Math.round((event.loaded / event.total) * 100));
    };
    request.onerror = () => reject(new Error("上传失败"));
    request.onload = () => {
      const payload = parseUploadPayload(request.responseText);
      if (request.status >= 200 && request.status < 300 && payload && "url" in payload) {
        onProgress?.(100);
        resolve(payload);
        return;
      }
      const message = payload && "error" in payload && typeof payload.error === "string" ? payload.error : "上传失败";
      reject(new ApiError(message, request.status));
    };
    request.send(body);
  });
}

function parseUploadPayload(value: string): UploadResponse | { error?: string } | null {
  try {
    return JSON.parse(value) as UploadResponse | { error?: string };
  } catch {
    return null;
  }
}

function uploadIdempotencyKey(attemptID: string, kind: UploadKind): string {
  return `web-media-${attemptID}-${kind}`.slice(0, 128);
}

function defaultContentType(kind: UploadKind): string {
  return kind === "video" ? "video/mp4" : "image/jpeg";
}
