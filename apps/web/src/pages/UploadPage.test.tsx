// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createVideo } from "../api/account";
import { uploadMediaFile } from "../api/upload";
import { UploadPage } from "./UploadPage";

vi.mock("../api/account", () => ({
  createVideo: vi.fn()
}));

vi.mock("../api/upload", () => ({
  uploadMediaFile: vi.fn()
}));

vi.mock("../router", () => ({
  useNavigate: () => vi.fn()
}));

vi.mock("../session", () => ({
  useSession: () => ({ token: "upload-token" })
}));

describe("upload page validation", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.clearAllMocks();
  });

  it("rejects a missing title before creating upload sessions", async () => {
    await act(async () => root.render(<UploadPage />));
    const title = container.querySelector<HTMLInputElement>('input[placeholder="输入视频标题"]');
    const description = container.querySelector<HTMLTextAreaElement>(
      'textarea[placeholder="输入视频简介"]'
    );
    expect(title?.required).toBe(true);
    expect(title?.maxLength).toBe(128);
    expect(description?.maxLength).toBe(512);

    const form = container.querySelector("form");
    await act(async () => {
      form?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });

    expect(container.textContent).toContain("请输入视频标题");
    expect(uploadMediaFile).not.toHaveBeenCalled();
    expect(createVideo).not.toHaveBeenCalled();
  });

  it("rejects an oversized UTF-8 title before uploading files", async () => {
    await act(async () => root.render(<UploadPage />));
    const title = container.querySelector<HTMLInputElement>('input[placeholder="输入视频标题"]');
    const setter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value"
    )?.set;
    await act(async () => {
      setter?.call(title, "中".repeat(43));
      title?.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const form = container.querySelector("form");
    await act(async () => {
      form?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    expect(container.textContent).toContain("视频标题过长");
    expect(uploadMediaFile).not.toHaveBeenCalled();
    expect(createVideo).not.toHaveBeenCalled();
  });
});
