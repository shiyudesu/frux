package interfaceshttpinteraction

import "time"

// createCommentRequest 是创建评论的 JSON 请求体。
type createCommentRequest struct {
	Content string `json:"content"`
}

// actionResponse 是点赞/收藏状态变更后的响应。
type actionResponse struct {
	VideoID       int64  `json:"video_id"`
	ActionType    string `json:"action_type"`
	Active        bool   `json:"active"`
	LikeCount     int    `json:"like_count"`
	FavoriteCount int    `json:"favorite_count"`
}

// commentResponse 是评论详情响应，创建评论时会额外返回 CommentCount。
type commentResponse struct {
	ID                   int64             `json:"id"`
	VideoID              int64             `json:"video_id"`
	UserID               int64             `json:"user_id,omitempty"`
	UserNickname         string            `json:"user_nickname"`
	UserAvatarURL        string            `json:"user_avatar_url"`
	RootCommentID        int64             `json:"root_comment_id"`
	ReplyToCommentID     int64             `json:"reply_to_comment_id"`
	ReplyToUserID        int64             `json:"reply_to_user_id"`
	ReplyToUserNickname  string            `json:"reply_to_user_nickname,omitempty"`
	ReplyToUserAvatarURL string            `json:"reply_to_user_avatar_url,omitempty"`
	Content              string            `json:"content"`
	Status               int               `json:"status"`
	Deleted              bool              `json:"deleted"`
	ReplyCount           int               `json:"reply_count"`
	ReplyPreviews        []commentResponse `json:"reply_previews"`
	LikeCount            int               `json:"like_count"`
	Liked                bool              `json:"liked"`
	CanDelete            bool              `json:"can_delete"`
	IsVideoAuthor        bool              `json:"is_video_author"`
	LikedByVideoAuthor   bool              `json:"liked_by_video_author"`
	HotScore             int64             `json:"hot_score"`
	CreatedAt            time.Time         `json:"created_at"`
	CommentCount         int               `json:"comment_count,omitempty"`
}

// commentListResponse 是评论游标分页响应。
type commentListResponse struct {
	Items        []commentResponse `json:"items"`
	NextCursor   string            `json:"next_cursor"`
	HasMore      bool              `json:"has_more"`
	CommentCount int               `json:"comment_count"`
	Sort         string            `json:"sort"`
}

type replyListResponse struct {
	RootCommentID int64             `json:"root_comment_id"`
	Items         []commentResponse `json:"items"`
	NextCursor    string            `json:"next_cursor"`
	HasMore       bool              `json:"has_more"`
	CommentCount  int               `json:"comment_count"`
}

type threadContextResponse struct {
	Root         commentResponse   `json:"root"`
	Replies      []commentResponse `json:"replies"`
	Target       commentResponse   `json:"target"`
	NextCursor   string            `json:"next_cursor"`
	HasMore      bool              `json:"has_more"`
	CommentCount int               `json:"comment_count"`
}

type commentLikeResponse struct {
	CommentID          int64 `json:"comment_id"`
	RootCommentID      int64 `json:"root_comment_id"`
	Liked              bool  `json:"liked"`
	LikeCount          int   `json:"like_count"`
	LikedByVideoAuthor bool  `json:"liked_by_video_author"`
}

// deleteCommentResponse 是删除评论后的状态响应。
type deleteCommentResponse struct {
	CommentID      int64 `json:"comment_id"`
	Status         int   `json:"status"`
	CommentCount   int   `json:"comment_count"`
	RootReplyCount int   `json:"root_reply_count"`
	DeletedCount   int   `json:"deleted_count"`
	ThreadHidden   bool  `json:"thread_hidden"`
	Tombstone      bool  `json:"tombstone"`
}
