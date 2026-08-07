// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, NetworkError, apiErrorMessage } from "./client";
import { uploadMediaFile } from "./upload";

interface XHRResult {
  status?: number;
  responseText?: string;
  networkError?: boolean;
}

class FakeXMLHttpRequest {
  static nextResult: XHRResult = {};

  status = 0;
  responseText = "";
  upload: { onprogress: ((event: ProgressEvent) => void) | null } = { onprogress: null };
  onerror: (() => void) | null = null;
  onload: (() => void) | null = null;

  open() {}

  setRequestHeader() {}

  send() {
    const result = FakeXMLHttpRequest.nextResult;
    queueMicrotask(() => {
      if (result.networkError) {
        this.onerror?.();
        return;
      }
      this.status = result.status ?? 200;
      this.responseText = result.responseText ?? "";
      this.onload?.();
    });
  }
}

describe("upload API errors", () => {
  beforeEach(() => {
    vi.stubGlobal("crypto", {
      subtle: {
        digest: vi.fn().mockResolvedValue(new ArrayBuffer(32))
      }
    });
    vi.stubGlobal("XMLHttpRequest", FakeXMLHttpRequest);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("preserves coded multipart upload failures", async () => {
    stubUploadSession({ mode: "multipart" });
    FakeXMLHttpRequest.nextResult = {
      status: 409,
      responseText: JSON.stringify({ code: "UPLOAD_SESSION_CONFLICT", error: "idempotency key conflict" })
    };

    await expect(upload()).rejects.toMatchObject({
      status: 409,
      code: "UPLOAD_SESSION_CONFLICT"
    });
  });

  it("keeps legacy and malformed multipart failures behind safe fallbacks", async () => {
    stubUploadSession({ mode: "multipart" });
    FakeXMLHttpRequest.nextResult = {
      status: 500,
      responseText: JSON.stringify({ error: "failed to save upload" })
    };
    await expect(upload()).rejects.toMatchObject({ status: 500, code: "" });

    stubUploadSession({ mode: "multipart" });
    FakeXMLHttpRequest.nextResult = { status: 500, responseText: "<html>failed</html>" };
    try {
      await upload();
      throw new Error("expected upload failure");
    } catch (error) {
      expect(apiErrorMessage(error, "上传失败")).toBe("上传失败，请稍后重试");
    }
  });

  it("rejects schema-invalid JSON without leaving the upload pending", async () => {
    stubUploadSession({ mode: "multipart" });
    FakeXMLHttpRequest.nextResult = { status: 500, responseText: JSON.stringify("upstream error") };

    await expect(upload()).rejects.toMatchObject({ status: 500, code: "" });

    stubUploadSession({ mode: "multipart" });
    FakeXMLHttpRequest.nextResult = {
      status: 200,
      responseText: JSON.stringify({ url: 42, kind: "video" })
    };
    await expect(upload()).rejects.toMatchObject({ status: 200, code: "" });
  });

  it("normalizes multipart network failures", async () => {
    stubUploadSession({ mode: "multipart" });
    FakeXMLHttpRequest.nextResult = { networkError: true };

    await expect(upload()).rejects.toBeInstanceOf(NetworkError);
  });

  it("normalizes direct object-storage HTTP failures", async () => {
    stubUploadSession({
      mode: "direct",
      id: "session-1",
      upload: { url: "https://objects.example/upload", method: "PUT", headers: {} }
    });
    FakeXMLHttpRequest.nextResult = { status: 503 };

    await expect(upload()).rejects.toMatchObject({
      status: 503,
      code: "UPLOAD_OBJECT_FAILED"
    } satisfies Partial<ApiError>);
  });

  it("derives fallback content types from file extensions", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify({
        mode: "direct",
        completed_asset_id: 7
      }), {
        status: 200,
        headers: { "Content-Type": "application/json" }
      }));
    vi.stubGlobal("fetch", fetchMock);

    await uploadWithFile(new File(["video"], "clip.webm"));
    await uploadWithFile(new File(["cover"], "cover.png"), "cover");

    const firstBody = JSON.parse(String((fetchMock.mock.calls[0][1] as RequestInit).body)) as Record<string, unknown>;
    const secondBody = JSON.parse(String((fetchMock.mock.calls[1][1] as RequestInit).body)) as Record<string, unknown>;
    expect(firstBody.content_type).toBe("video/webm");
    expect(secondBody.content_type).toBe("image/png");
  });
});

function upload() {
  const file = new File(["video"], "video.mp4", { type: "video/mp4" });
  return uploadWithFile(file);
}

function uploadWithFile(file: File, kind: "video" | "cover" = "video") {
  Object.defineProperty(file, "arrayBuffer", {
    configurable: true,
    value: () => Promise.resolve(new TextEncoder().encode(file.name).buffer)
  });
  return uploadMediaFile(file, kind, "token", `attempt-${kind}`);
}

function stubUploadSession(value: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  })));
}
