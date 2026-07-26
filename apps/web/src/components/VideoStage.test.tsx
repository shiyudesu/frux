import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { FeedVideo } from "../types";
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
});
