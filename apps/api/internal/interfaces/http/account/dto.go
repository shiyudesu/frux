package interfaceshttpaccount

// 账号注册请求
type RegisterRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

// 账号登录请求
type LoginByPasswordRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// 用户资料更新请求
type UpdateProfileRequest struct {
	Nickname       *string                      `json:"nickname"`
	AvatarURL      *string                      `json:"avatar_url"`
	Bio            *string                      `json:"bio"`
	Gender         *int                         `json:"gender"`
	ProfileSetting *UpdateProfileSettingRequest `json:"profile_settings"`
}

type UpdateProfileSettingRequest struct {
	LikedVisibility    *string `json:"liked_visibility"`
	FavoriteVisibility *string `json:"favorite_visibility"`
}

type profileSettingResponse struct {
	LikedVisibility    string `json:"liked_visibility"`
	FavoriteVisibility string `json:"favorite_visibility"`
}

// 账号登录响应
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
}

// 用户信息响应
type userProfileResponse struct {
	ID                int64                   `json:"id"`
	Account           string                  `json:"account"`
	Nickname          string                  `json:"nickname"`
	AvatarURL         string                  `json:"avatar_url"`
	Bio               string                  `json:"bio"`
	Status            int                     `json:"status"`
	Role              string                  `json:"role"`
	FollowingCount    int                     `json:"following_count"`
	FollowerCount     int                     `json:"follower_count"`
	WorkCount         int                     `json:"work_count"`
	Gender            int                     `json:"gender"`
	PublicWorkCount   int                     `json:"public_work_count"`
	PrivateWorkCount  int                     `json:"private_work_count"`
	ReceivedLikeCount int                     `json:"received_like_count"`
	ProfileSettings   *profileSettingResponse `json:"profile_settings,omitempty"`
}

// 公开用户信息响应，隐藏账号、角色和状态等内部字段。
type publicUserProfileResponse struct {
	ID                int64  `json:"id"`
	Nickname          string `json:"nickname"`
	AvatarURL         string `json:"avatar_url"`
	Bio               string `json:"bio"`
	FollowingCount    int    `json:"following_count"`
	FollowerCount     int    `json:"follower_count"`
	WorkCount         int    `json:"work_count"`
	Gender            int    `json:"gender"`
	PublicWorkCount   int    `json:"public_work_count"`
	ReceivedLikeCount int    `json:"received_like_count"`
	LikedVideosPublic bool   `json:"liked_videos_public"`
}
