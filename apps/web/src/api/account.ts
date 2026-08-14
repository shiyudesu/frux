import type {
  CreateVideoRequest,
  LoginRequest,
  PasswordChangeRequest,
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
    body,
    credentials: "same-origin"
  });
}

export function refreshSession(): Promise<TokenResponse> {
  return apiRequest<TokenResponse>("/api/sessions/current/refresh", {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store"
  });
}

export function logoutSession(): Promise<void> {
  return apiRequest<void>("/api/sessions/current", {
    method: "DELETE",
    credentials: "same-origin",
    keepalive: true
  });
}

export function changeMyPassword(
  body: PasswordChangeRequest,
  token?: string
): Promise<TokenResponse> {
  return apiRequest<TokenResponse>("/api/users/me/password", {
    method: "PUT",
    token,
    auth: "consumer",
    retryAuth: false,
    body,
    credentials: "same-origin"
  });
}

export function fetchMyProfile(token: string): Promise<UserProfile> {
  return apiRequest<UserProfile>("/api/users/me", { token, auth: "consumer" });
}

export function fetchMyProfileWithAccessToken(token: string): Promise<UserProfile> {
  return apiRequest<UserProfile>("/api/users/me", {
    token,
    retryAuth: false,
    cache: "no-store"
  });
}

export function updateMyProfile(token: string, body: UpdateProfileRequest): Promise<UserProfile> {
  return apiRequest<UserProfile>("/api/users/me", {
    method: "PATCH",
    token,
    auth: "consumer",
    body
  });
}

export function fetchProfileSettings(token: string): Promise<ProfileSettings> {
  return apiRequest<ProfileSettings>("/api/users/me/profile-settings", { token, auth: "consumer" });
}

export function updateProfileSettings(
  token: string,
  body: UpdateProfileSettingsRequest
): Promise<ProfileSettings> {
  return apiRequest<ProfileSettings>("/api/users/me/profile-settings", {
    method: "PATCH",
    token,
    auth: "consumer",
    body
  });
}

export function fetchMyVideos(token: string, limit = 12): Promise<VideoListResponse> {
  return apiRequest<VideoListResponse>(`/api/users/me/videos?limit=${limit}`, {
    token,
    auth: "consumer"
  });
}

export function fetchPublicProfile(userID: number): Promise<PublicUserProfile> {
  return apiRequest<PublicUserProfile>(`/api/users/${userID}`);
}

export function fetchUserVideos(userID: number, limit = 24, offset = 0): Promise<VideoListResponse> {
  return apiRequest<VideoListResponse>(
    `/api/users/${userID}/videos?limit=${limit}&offset=${offset}`
  );
}

export function fetchVideo(videoID: number): Promise<Video> {
  return apiRequest<Video>(`/api/videos/${videoID}`);
}

export function createVideo(token: string, body: CreateVideoRequest, idempotencyKey: string): Promise<Video> {
  return apiRequest<Video>("/api/videos", {
    method: "POST",
    token,
    auth: "consumer",
    headers: {
      "Idempotency-Key": idempotencyKey
    },
    body
  });
}
