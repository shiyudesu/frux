// 账号域 API：注册/登录/登出、个人资料、作品列表、发布视频、公开资料。
import type {
  CreateVideoRequest,
  LoginRequest,
  ProfileSettings,
  PublicUserProfile,
  RegisterRequest,
  TokenResponse,
  UpdateProfileRequest,
  UpdateProfileSettingsRequest,
  UserProfile,
  Video,
  VideoListResponse
} from "../types";
import { apiRequest } from "./client";

export function registerUser(body: RegisterRequest): Promise<unknown> {
  return apiRequest("/api/users", {
    method: "POST",
    body
  });
}

export function login(body: LoginRequest): Promise<TokenResponse> {
  return apiRequest<TokenResponse>("/api/sessions", {
    method: "POST",
    body
  });
}

export function logoutSession(token?: string): Promise<void> {
  return apiRequest<void>("/api/sessions/current", {
    method: "DELETE",
    token
  });
}

export function fetchMyProfile(token: string): Promise<UserProfile> {
  return apiRequest<UserProfile>("/api/users/me", { token });
}

export function updateMyProfile(token: string, body: UpdateProfileRequest): Promise<UserProfile> {
  return apiRequest<UserProfile>("/api/users/me", {
    method: "PATCH",
    token,
    body
  });
}

export function fetchProfileSettings(token: string): Promise<ProfileSettings> {
  return apiRequest<ProfileSettings>("/api/users/me/profile-settings", { token });
}

export function updateProfileSettings(
  token: string,
  body: UpdateProfileSettingsRequest
): Promise<ProfileSettings> {
  return apiRequest<ProfileSettings>("/api/users/me/profile-settings", {
    method: "PATCH",
    token,
    body
  });
}

export function fetchMyVideos(token: string, limit = 12): Promise<VideoListResponse> {
  return apiRequest<VideoListResponse>(`/api/users/me/videos?limit=${limit}`, { token });
}

export function fetchPublicProfile(userID: number): Promise<PublicUserProfile> {
  return apiRequest<PublicUserProfile>(`/api/users/${userID}`);
}

export function fetchUserVideos(userID: number, limit = 24): Promise<VideoListResponse> {
  return apiRequest<VideoListResponse>(`/api/users/${userID}/videos?limit=${limit}`);
}

export function fetchVideo(videoID: number): Promise<Video> {
  return apiRequest<Video>(`/api/videos/${videoID}`);
}

export function createVideo(token: string, body: CreateVideoRequest, idempotencyKey: string): Promise<Video> {
  return apiRequest<Video>("/api/videos", {
    method: "POST",
    token,
    headers: {
      "Idempotency-Key": idempotencyKey
    },
    body
  });
}
