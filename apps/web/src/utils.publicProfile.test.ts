// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PUBLIC_PROFILE_KEY } from "./constants";
import type { Comment, StoredPublicProfile } from "./types";
import {
  openPublicProfile,
  profileFromComment,
  readPublicProfile,
  readPublicProfiles,
  savePublicProfile
} from "./utils";

describe("public profile cache privacy", () => {
  beforeEach(() => localStorage.clear());

  it("sanitizes and rewrites legacy account-bearing entries", () => {
    localStorage.setItem(PUBLIC_PROFILE_KEY, JSON.stringify({
      "7": {
        id: 7,
        account: "private-login",
        nickname: "公开昵称",
        avatar_url: "/avatar.png",
        bio: "简介",
        follower_count: 3,
        internal_note: "remove-me"
      },
      invalid: {
        account: "invalid-private-login",
        nickname: "无效条目"
      }
    }));

    expect(readPublicProfiles()).toEqual({
      "7": {
        id: 7,
        nickname: "公开昵称",
        avatar_url: "/avatar.png",
        bio: "简介",
        follower_count: 3
      }
    });
    expect(localStorage.getItem(PUBLIC_PROFILE_KEY)).toBe(JSON.stringify({
      "7": {
        id: 7,
        nickname: "公开昵称",
        avatar_url: "/avatar.png",
        bio: "简介",
        follower_count: 3
      }
    }));
  });

  it("projects runtime writes and comment navigation into the public shape", () => {
    const legacyProfile: StoredPublicProfile & { account: string; internal_note: string } = {
      id: 7,
      account: "private-login",
      nickname: "公开昵称",
      avatar_url: "/avatar.png",
      bio: "简介",
      internal_note: "remove-me"
    };
    savePublicProfile(legacyProfile);

    const navigate = vi.fn();
    openPublicProfile(profileFromComment(comment(9)), navigate);

    expect(readPublicProfile(7)).toEqual({
      id: 7,
      nickname: "公开昵称",
      avatar_url: "/avatar.png",
      bio: "简介"
    });
    expect(readPublicProfile(9)).toEqual({
      id: 9,
      nickname: "评论用户",
      avatar_url: "/comment-avatar.png",
      bio: ""
    });
    expect(navigate).toHaveBeenCalledWith("/users/9");
    expect(localStorage.getItem(PUBLIC_PROFILE_KEY)).not.toContain("private-login");
    expect(localStorage.getItem(PUBLIC_PROFILE_KEY)).not.toContain("internal_note");
    expect(localStorage.getItem(PUBLIC_PROFILE_KEY)).not.toContain("account");
  });
});

function comment(id: number): Comment {
  return {
    id,
    video_id: 3,
    user_id: 9,
    user_nickname: "评论用户",
    user_avatar_url: "/comment-avatar.png",
    root_comment_id: 0,
    reply_to_comment_id: 0,
    reply_to_user_id: 0,
    reply_to_user_nickname: "",
    reply_to_user_avatar_url: "",
    content: "评论内容",
    status: 1,
    deleted: false,
    reply_count: 0,
    reply_previews: [],
    like_count: 0,
    liked: false,
    can_delete: false,
    is_video_author: false,
    liked_by_video_author: false,
    hot_score: 0,
    created_at: "2026-08-14T00:00:00Z"
  };
}
