import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  ADMIN_AUTH_INVALID_EVENT,
  NetworkError,
  UserFacingError,
  apiErrorMessage,
  apiRequest,
  isUnauthorized
} from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("safe API error messages", () => {
  it("maps known codes without exposing diagnostic text", () => {
    expect(apiErrorMessage(
      new ApiError("invalid credentials", 401, "AUTH_INVALID_CREDENTIALS"),
      "登录失败"
    )).toBe("账号或密码错误，请重新输入");
    expect(apiErrorMessage(
      new ApiError("account already exists", 409, "ACCOUNT_ALREADY_EXISTS"),
      "注册失败"
    )).toBe("该账号已注册，请直接登录或更换账号");
    expect(apiErrorMessage(
      new ApiError("video codec is unsupported", 400, "UPLOAD_VALIDATION_FAILED"),
      "发布失败"
    )).toBe("上传文件不符合要求，请检查后重试");
    expect(apiErrorMessage(
      new ApiError("cover upload exceeds 20 MiB", 400, "UPLOAD_COVER_TOO_LARGE"),
      "发布失败"
    )).toBe("封面不能超过 20 MB");
    expect(apiErrorMessage(
      new ApiError("unsupported upload file type", 400, "UPLOAD_FILE_TYPE_INVALID"),
      "发布失败"
    )).toBe("文件格式不受支持，请重新选择");
  });

  it("uses safe fallbacks for missing and unknown codes", () => {
    expect(apiErrorMessage(new ApiError("内部中文详情", 400), "保存失败")).toBe("保存失败");
    expect(apiErrorMessage(new ApiError("internal server error", 500), "保存失败"))
      .toBe("保存失败，请稍后重试");
    expect(apiErrorMessage(new ApiError("unknown", 429), "提交失败"))
      .toBe("操作过于频繁，请稍后重试");
  });

  it("distinguishes trusted local validation from arbitrary errors", () => {
    expect(apiErrorMessage(new UserFacingError("请选择视频文件"), "发布失败"))
      .toBe("请选择视频文件");
    expect(apiErrorMessage(new Error("sensitive implementation detail"), "发布失败"))
      .toBe("发布失败");
  });

  it("normalizes network failures without hiding programming errors", () => {
    expect(apiErrorMessage(new NetworkError("socket closed"), "加载失败"))
      .toBe("网络连接失败，请检查网络后重试");
    expect(apiErrorMessage(new TypeError("Cannot read properties of undefined"), "加载失败"))
      .toBe("加载失败");
  });

  it("keeps invalid credentials separate from expired authenticated sessions", () => {
    expect(isUnauthorized(new ApiError("invalid access token", 401, "AUTH_INVALID_ACCESS_TOKEN"))).toBe(true);
    expect(isUnauthorized(new ApiError("invalid credentials", 401, "AUTH_INVALID_CREDENTIALS"))).toBe(false);
  });
});

describe("typed API errors", () => {
  it("preserves status, code, and legacy diagnostic text", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "VIDEO_NOT_FOUND",
      error: "video not found"
    }), {
      status: 404,
      headers: { "Content-Type": "application/json" }
    })));

    const request = apiRequest("/api/videos/42");
    await expect(request).rejects.toMatchObject({
      status: 404,
      code: "VIDEO_NOT_FOUND",
      diagnosticMessage: "video not found"
    });
  });

  it("keeps legacy responses safe when no code is present", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ error: "internal server error" }),
      { status: 500, headers: { "Content-Type": "application/json" } }
    )));

    try {
      await apiRequest("/api/messages");
      throw new Error("expected request failure");
    } catch (error) {
      expect(apiErrorMessage(error, "消息加载失败")).toBe("消息加载失败，请稍后重试");
    }
  });

  it("wraps actual fetch rejections as network errors", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    await expect(apiRequest("/api/feed-items")).rejects.toBeInstanceOf(NetworkError);
  });

  it("announces authoritative admin authentication failures only for protected admin APIs", async () => {
    const dispatchEvent = vi.fn();
    vi.stubGlobal("window", { dispatchEvent });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "ADMIN_AUTH_INVALID_ACCESS_TOKEN",
      error: "invalid admin access token"
    }), {
      status: 401,
      headers: { "Content-Type": "application/json" }
    })));

    await expect(apiRequest("/api/admin/me", {
      token: "rejected-token"
    })).rejects.toBeInstanceOf(ApiError);
    expect(dispatchEvent).toHaveBeenCalledTimes(1);
    const event = dispatchEvent.mock.calls[0][0] as CustomEvent<{ token: string }>;
    expect(event.type).toBe(ADMIN_AUTH_INVALID_EVENT);
    expect(event.detail.token).toBe("rejected-token");

    dispatchEvent.mockClear();
    await expect(apiRequest("/api/admin/auth/login", {
      method: "POST",
      body: { account: "x", password: "y" }
    })).rejects.toBeInstanceOf(ApiError);
    expect(dispatchEvent).not.toHaveBeenCalled();
  });
});
