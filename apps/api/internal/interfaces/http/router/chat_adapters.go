package interfaceshttprouter

import (
	"context"
	"errors"

	applicationchat "github.com/shiyudesu/frux/internal/application/chat"
	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	applicationrelation "github.com/shiyudesu/frux/internal/application/relation"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainchat "github.com/shiyudesu/frux/internal/domain/chat"
	domainrelation "github.com/shiyudesu/frux/internal/domain/relation"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type chatAccountAdapter struct {
	reader domainaccount.ConsumerDisplayReader
}

func (a chatAccountAdapter) GetParticipant(ctx context.Context, userID int64) (*domainchat.Participant, error) {
	display, err := a.reader.GetConsumerDisplay(ctx, userID)
	if err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return domainchat.UnavailableParticipant(userID), nil
		}
		return nil, err
	}
	if display == nil || display.Status != domainaccount.StatusNormal || display.Role != domainaccount.RoleUser {
		return domainchat.UnavailableParticipant(userID), nil
	}
	return domainchat.RestoreParticipant(
		display.UserID, display.Nickname, display.AvatarURL, display.Bio, true,
	), nil
}

func (a chatAccountAdapter) BatchGetParticipants(ctx context.Context, userIDs []int64) (map[int64]*domainchat.Participant, error) {
	displays, err := a.reader.BatchGetConsumerDisplays(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]*domainchat.Participant, len(userIDs))
	for _, userID := range userIDs {
		display := displays[userID]
		if display == nil || display.Status != domainaccount.StatusNormal || display.Role != domainaccount.RoleUser {
			result[userID] = domainchat.UnavailableParticipant(userID)
			continue
		}
		result[userID] = domainchat.RestoreParticipant(
			display.UserID, display.Nickname, display.AvatarURL, display.Bio, true,
		)
	}
	return result, nil
}

var _ applicationchat.AccountReader = chatAccountAdapter{}

type chatRelationAdapter struct {
	service *applicationrelation.Service
}

func (a chatRelationAdapter) AreMutuallyFollowing(ctx context.Context, firstUserID, secondUserID int64) (bool, error) {
	return a.service.AreMutuallyFollowing(ctx, firstUserID, secondUserID)
}

func (a chatRelationAdapter) ListMutualRecipients(ctx context.Context, userID int64, query string, cursor *domainchat.RecipientCursor, limit int) ([]*domainchat.Recipient, error) {
	var relationCursor *domainrelation.ListCursor
	if cursor != nil {
		relationCursor = &domainrelation.ListCursor{
			Version: cursor.Version, Kind: domainrelation.ListKindMutual,
			Query: cursor.Query, FollowedAt: cursor.FollowedAt, UserID: cursor.UserID,
		}
	}
	result, err := a.service.ListMutualRecipients(ctx, userID, query, relationCursor, limit)
	if err != nil {
		return nil, err
	}
	items := make([]*domainchat.Recipient, 0, len(result.Items))
	for _, item := range result.Items {
		if item == nil {
			continue
		}
		items = append(items, &domainchat.Recipient{
			UserID: item.UserID, Nickname: item.Nickname, AvatarURL: item.AvatarURL,
			Bio: item.Bio, FollowedAt: item.FollowedAt,
		})
	}
	return items, nil
}

var _ applicationchat.MutualFollowReader = chatRelationAdapter{}

type chatVideoAdapter struct {
	reader domainvideo.Repository
}

func (a chatVideoAdapter) ValidatePublicVideo(ctx context.Context, videoID int64) (*domainchat.VideoCard, error) {
	video, err := a.reader.FindByID(ctx, videoID)
	if err != nil {
		if errors.Is(err, domainvideo.ErrVideoNotFound) {
			return nil, domainchat.ErrVideoUnavailable
		}
		return nil, err
	}
	if video == nil || !video.IsPubliclyReadable() {
		return nil, domainchat.ErrVideoUnavailable
	}
	return videoCardFromDomain(video), nil
}

func (a chatVideoAdapter) BatchHydratePublicVideos(ctx context.Context, videoIDs []int64) (map[int64]*domainchat.VideoCard, error) {
	result := make(map[int64]*domainchat.VideoCard, len(videoIDs))
	if len(videoIDs) == 0 {
		return result, nil
	}
	if batchReader, ok := a.reader.(interface {
		BatchGetReadable(context.Context, int64, []int64, bool) (map[int64]*domainvideo.Video, error)
	}); ok {
		videos, err := batchReader.BatchGetReadable(ctx, 0, videoIDs, true)
		if err != nil {
			return nil, err
		}
		for videoID, video := range videos {
			if video != nil && video.IsPubliclyReadable() {
				result[videoID] = videoCardFromDomain(video)
			}
		}
		return result, nil
	}
	for _, videoID := range videoIDs {
		video, err := a.reader.FindByID(ctx, videoID)
		if err != nil {
			if errors.Is(err, domainvideo.ErrVideoNotFound) {
				continue
			}
			return nil, err
		}
		if video == nil || !video.IsPubliclyReadable() {
			continue
		}
		result[videoID] = videoCardFromDomain(video)
	}
	return result, nil
}

func videoCardFromDomain(video *domainvideo.Video) *domainchat.VideoCard {
	return domainchat.RestoreVideoCard(
		video.ID, video.AuthorID, video.Title, video.CoverURL, video.MediaURL, true,
	)
}

var _ applicationchat.VideoReader = chatVideoAdapter{}

type notificationUnreadAdapter struct {
	service *applicationmessage.Service
}

func (a notificationUnreadAdapter) CountUnread(ctx context.Context, userID int64) (int, error) {
	result, err := a.service.CountUnread(ctx, userID)
	if err != nil {
		return 0, err
	}
	return result.UnreadCount, nil
}

var _ applicationchat.NotificationUnreadReader = notificationUnreadAdapter{}
