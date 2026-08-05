import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { createInitialPlayerState } from "../player";
import { FeedPlayerControls } from "./FeedPlayerControls";

const callbacks = {
  onTogglePlayback: () => {},
  onToggleMute: () => {},
  onSeek: () => {},
  onSelectQuality: () => {},
  onSelectRate: () => {},
  onToggleContinuousPlay: () => {},
  onRetry: () => {},
  onToggleFullscreen: () => {}
};

describe("FeedPlayerControls", () => {
  it("renders accessible quality, speed, and continuous-play controls", () => {
    const html = renderToStaticMarkup(
      <FeedPlayerControls
        {...callbacks}
        fullscreen={false}
        continuousPlay
        state={{
          ...createInitialPlayerState(),
          status: "playing",
          duration: 60,
          currentTime: 12,
          playbackRate: 1.25,
          selectedQuality: "720p",
          qualities: [
            { id: "720p", label: "720p", selected: true, active: true }
          ]
        }}
      />
    );

    expect(html).toContain('aria-label="清晰度"');
    expect(html).toContain('aria-label="播放速度"');
    expect(html).toContain('aria-pressed="true"');
    expect(html).toContain("720p · 当前");
    expect(html).toContain("1.25x");
  });

  it("announces buffering and exposes retry only for recoverable errors", () => {
    const buffering = renderToStaticMarkup(
      <FeedPlayerControls
        {...callbacks}
        fullscreen={false}
        continuousPlay={false}
        state={{ ...createInitialPlayerState(), status: "buffering", intendedPlay: true }}
      />
    );
    expect(buffering).toContain('role="status"');
    expect(buffering).toContain("缓冲中");
    expect(buffering).toContain('aria-label="暂停"');

    const failed = renderToStaticMarkup(
      <FeedPlayerControls
        {...callbacks}
        fullscreen={false}
        continuousPlay={false}
        state={{
          ...createInitialPlayerState(),
          status: "error",
          error: {
            category: "network",
            code: "network",
            message: "A network error interrupted media loading.",
            recoverable: true
          }
        }}
      />
    );
    expect(failed).toContain("视频加载中断，请检查网络后重试");
    expect(failed).not.toContain("A network error interrupted media loading.");
    expect(failed).toContain("重试");
  });
});
