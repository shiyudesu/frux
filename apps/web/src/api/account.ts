// 账号域 API：注册/登录/登出、个人资料、作品列表、发布视频、公开资料。
import type {
  CreateVideoRequest,
  LoginRequest,
  PublicUserProfile,
  RegisterRequest,
  TokenResponse,
  UpdateProfileRequest,
  UserProfile,
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

export function logoutSession(token: string): Promise<unknown> {
  return apiRequest("/api/sessions/current", {
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

export function fetchMyVideos(token: string, limit = 12): Promise<VideoListResponse> {
  return apiRequest<VideoListResponse>(`/api/users/me/videos?limit=${limit}`, { token });
}

export function fetchPublicProfile(userID: number): Promise<PublicUserProfile> {
  return apiRequest<PublicUserProfile>(`/api/users/${userID}`);
}

export function fetchUserVideos(userID: number, limit = 24): Promise<VideoListResponse> {
  return apiRequest<VideoListResponse>(`/api/users/${userID}/videos?limit=${limit}`);
}

export function createVideo(token: string, body: CreateVideoRequest): Promise<unknown> {
  return apiRequest("/api/videos", {
    method: "POST",
    token,
    headers: {
      "Idempotency-Key": `web-upload-${Date.now()}`
    },
    body
  });
}
