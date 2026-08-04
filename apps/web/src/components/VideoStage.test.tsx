import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { FeedVideo } from "../types";
import { feedbackStateKey } from "./FeedActionRail";
import { VideoStage } from "./VideoStage";

const baseItem: FeedVideo = {
  video_id: 1,
  author_id: 2,
  title: "image item",
  media_url: "/uploads/cover/image.jpg",
  cover_url: "/uploads/cover/image.jpg",
  like_count: 0,
  comment_count: 0,
  favorite_count: 0,
  liked: false,
  favorited: false,
  author: "author",
  avatar_url: "",
  description: "",
  feed_scene: "timeline",
  request_id: "request-1"
};

const callbacks = {
  onLike: () => {},
  onComment: () => {},
  onFavorite: () => {},
  onFollow: () => {},
  onOpenAuthor: () => {},
  onUpdatePlayerPreferences: () => {},
  onContinuousAdvance: () => {}
};

describe("VideoStage", () => {
  it("remounts recommendation feedback state for each video", () => {
    expect(feedbackStateKey({ video_id: 1 })).not.toBe(feedbackStateKey({ video_id: 2 }));
  });

  it("keeps image fallback visible without video-only controls", () => {
    const html = renderToStaticMarkup(
      <VideoStage
        {...callbacks}
        item={baseItem}
        active
        liked={false}
        favorited={false}
        following={false}
        followBusy={false}
        ownVideo={false}
        followError=""
        playerPreferences={{ quality: "auto", playbackRate: 1, continuousPlay: false }}
      />
    );

    expect(html).toContain("portrait-media");
    expect(html).not.toContain('data-ui="player-controls"');
  });

  it("can expose playback and comments without unknown social viewer state", () => {
    const html = renderToStaticMarkup(
      <VideoStage
        {...callbacks}
        item={baseItem}
        active
        showSocialActions={false}
        liked={false}
        favorited={false}
        following={false}
        followBusy={false}
        ownVideo={false}
        followError=""
        playerPreferences={{ quality: "auto", playbackRate: 1, continuousPlay: false }}
      />
    );

    expect(html).toContain('aria-label="查看评论"');
    expect(html).not.toContain('aria-label="点赞"');
    expect(html).not.toContain('aria-label="收藏"');
    expect(html).not.toContain('aria-label="关注作者"');
  });

  it("uses typed playback sources when the public media URL has no extension", () => {
    const html = renderToStaticMarkup(
      <VideoStage
        {...callbacks}
        item={{
          ...baseItem,
          media_url: "/media/assets/opaque-key",
          playback_sources: [{ type: "mp4", url: "/media/assets/opaque-key", role: "baseline" }]
        }}
        active
        liked={false}
        favorited={false}
        following={false}
        followBusy={false}
        ownVideo={false}
        followError=""
        playerPreferences={{ quality: "auto", playbackRate: 1, continuousPlay: false }}
      />
    );

    expect(html).toContain('data-ui="player-controls"');
    expect(html).not.toContain("portrait-media");
  });

});
