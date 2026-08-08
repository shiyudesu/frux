// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { AdaptiveFeedMediaResource } from "./adaptiveFeedMedia";

describe("AdaptiveFeedMediaResource browser media policy", () => {
  let resource: AdaptiveFeedMediaResource | null = null;

  afterEach(() => {
    resource?.destroy();
    resource = null;
  });

  it("uses only the custom player controls and suppresses the browser media menu", () => {
    const host = document.createElement("div");
    resource = new AdaptiveFeedMediaResource();
    resource.mount(host, "stage-media");
    const video = host.querySelector("video");
    const contextMenu = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });

    video?.dispatchEvent(contextMenu);

    expect(video).not.toBeNull();
    expect(video?.controls).toBe(false);
    expect(video?.disablePictureInPicture).toBe(true);
    expect(video?.disableRemotePlayback).toBe(true);
    expect(video?.getAttribute("controlslist")).toBe("nodownload noremoteplayback");
    expect(contextMenu.defaultPrevented).toBe(true);
  });
});
