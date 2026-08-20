package domainchat

import (
	"strings"
	"time"
	"unicode/utf8"
)

type MessageKind string

const (
	MessageKindText  MessageKind = "TEXT"
	MessageKindVideo MessageKind = "VIDEO"
)

type EligibilityReason string

const (
	EligibilityReasonEligible           EligibilityReason = "ELIGIBLE"
	EligibilityReasonSelf               EligibilityReason = "SELF"
	EligibilityReasonNotMutual          EligibilityReason = "NOT_MUTUAL_FOLLOW"
	EligibilityReasonAccountUnavailable EligibilityReason = "ACCOUNT_UNAVAILABLE"
)

const (
	DefaultLimit        = 20
	MaxLimit            = 100
	MaxTextCodePoints   = 2000
	MaxTextRequestBytes = 8192
	MaxQueryCodePoints  = 64
	MaxIdempotencyKey   = 128
	CursorVersion       = 1
)

type Conversation struct {
	ID            int64
	LowerUserID   int64
	HigherUserID  int64
	LastMessageID int64
	LastMessageAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Member struct {
	ConversationID    int64
	UserID            int64
	LastReadMessageID int64
	LastReadAt        *time.Time
	UnreadCount       int
	MutedAt           *time.Time
	HiddenAt          *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Message struct {
	ID             int64
	ConversationID int64
	SenderID       int64
	Kind           MessageKind
	Text           string
	VideoID        int64
	IdempotencyKey string
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

func (m *Message) SameSendPayload(other *Message) bool {
	if m == nil || other == nil {
		return false
	}
	return m.ConversationID == other.ConversationID &&
		m.SenderID == other.SenderID &&
		m.Kind == other.Kind &&
		m.Text == other.Text &&
		m.VideoID == other.VideoID &&
		m.IdempotencyKey == other.IdempotencyKey
}

type Participant struct {
	UserID    int64
	Nickname  string
	AvatarURL string
	Bio       string
	Available bool
}

type VideoCard struct {
	VideoID         int64
	Available       bool
	Title           string
	CoverURL        string
	MediaURL        string
	AuthorID        int64
	AuthorNickname  string
	AuthorAvatarURL string
}

type Recipient struct {
	UserID         int64
	Nickname       string
	AvatarURL      string
	Bio            string
	FollowedAt     time.Time
	ConversationID int64
}

type ConversationCursor struct {
	Version        int
	LastMessageID  int64
	ConversationID int64
}

type HistoryCursor struct {
	Version        int
	ConversationID int64
	MessageID      int64
}

type RecipientCursor struct {
	Version    int
	Query      string
	FollowedAt time.Time
	UserID     int64
}

type Eligibility struct {
	Eligible       bool
	Reason         EligibilityReason
	ConversationID int64
}

type ConversationItem struct {
	Conversation *Conversation
	Member       *Member
	Counterpart  *Participant
	LastMessage  *Message
}

type MessageItem struct {
	Message *Message
	Sender  *Participant
	Video   *VideoCard
}

func CanonicalPair(first, second int64) (int64, int64, error) {
	if first <= 0 {
		return 0, 0, ErrInvalidUserID
	}
	if second <= 0 {
		return 0, 0, ErrInvalidTargetUserID
	}
	if first == second {
		return 0, 0, ErrSelfConversation
	}
	if first < second {
		return first, second, nil
	}
	return second, first, nil
}

func NewConversation(first, second int64, now time.Time) (*Conversation, error) {
	lower, higher, err := CanonicalPair(first, second)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	return &Conversation{
		LowerUserID:  lower,
		HigherUserID: higher,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func NewTextMessage(conversationID, senderID int64, text, idempotencyKey string, now time.Time) (*Message, error) {
	if conversationID <= 0 {
		return nil, ErrInvalidConversationID
	}
	if senderID <= 0 {
		return nil, ErrInvalidUserID
	}
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptyText
	}
	if !utf8.ValidString(text) {
		return nil, ErrInvalidMessageShape
	}
	if utf8.RuneCountInString(text) > MaxTextCodePoints || len([]byte(text)) > MaxTextRequestBytes {
		return nil, ErrTextTooLong
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &Message{
		ConversationID: conversationID,
		SenderID:       senderID,
		Kind:           MessageKindText,
		Text:           text,
		IdempotencyKey: key,
		CreatedAt:      now.UTC(),
	}, nil
}

func NewVideoMessage(conversationID, senderID, videoID int64, idempotencyKey string, now time.Time) (*Message, error) {
	if conversationID <= 0 {
		return nil, ErrInvalidConversationID
	}
	if senderID <= 0 {
		return nil, ErrInvalidUserID
	}
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &Message{
		ConversationID: conversationID,
		SenderID:       senderID,
		Kind:           MessageKindVideo,
		VideoID:        videoID,
		IdempotencyKey: key,
		CreatedAt:      now.UTC(),
	}, nil
}

func normalizeIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrIdempotencyKeyRequired
	}
	if len(value) > MaxIdempotencyKey {
		return "", ErrIdempotencyKeyTooLong
	}
	return value, nil
}

func NormalizeQuery(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if utf8.RuneCountInString(value) > MaxQueryCodePoints {
		return "", ErrInvalidQuery
	}
	return value, nil
}

func NormalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

func RestoreConversation(id, lower, higher, lastMessageID int64, lastMessageAt *time.Time, createdAt, updatedAt time.Time) *Conversation {
	return &Conversation{
		ID:            id,
		LowerUserID:   lower,
		HigherUserID:  higher,
		LastMessageID: lastMessageID,
		LastMessageAt: cloneTime(lastMessageAt),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
}

func RestoreMember(conversationID, userID, lastReadMessageID int64, lastReadAt *time.Time, unreadCount int, mutedAt, hiddenAt *time.Time, createdAt, updatedAt time.Time) *Member {
	if unreadCount < 0 {
		unreadCount = 0
	}
	return &Member{
		ConversationID:    conversationID,
		UserID:            userID,
		LastReadMessageID: lastReadMessageID,
		LastReadAt:        cloneTime(lastReadAt),
		UnreadCount:       unreadCount,
		MutedAt:           cloneTime(mutedAt),
		HiddenAt:          cloneTime(hiddenAt),
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}
}

func RestoreMessage(id, conversationID, senderID int64, kind MessageKind, text string, videoID int64, idempotencyKey string, revokedAt *time.Time, createdAt time.Time) *Message {
	if kind != MessageKindVideo {
		kind = MessageKindText
	}
	if kind == MessageKindText {
		videoID = 0
	} else {
		text = ""
	}
	return &Message{
		ID: id, ConversationID: conversationID, SenderID: senderID, Kind: kind,
		Text: strings.TrimSpace(text), VideoID: videoID, IdempotencyKey: strings.TrimSpace(idempotencyKey),
		RevokedAt: cloneTime(revokedAt), CreatedAt: createdAt,
	}
}

func RestoreParticipant(userID int64, nickname, avatarURL, bio string, available bool) *Participant {
	return &Participant{
		UserID: userID, Nickname: strings.TrimSpace(nickname),
		AvatarURL: strings.TrimSpace(avatarURL), Bio: strings.TrimSpace(bio), Available: available,
	}
}

func UnavailableParticipant(userID int64) *Participant {
	return RestoreParticipant(userID, "", "", "", false)
}

func RestoreVideoCard(videoID, authorID int64, title, coverURL, mediaURL string, available bool) *VideoCard {
	return &VideoCard{
		VideoID: videoID, AuthorID: authorID, Title: strings.TrimSpace(title),
		CoverURL: strings.TrimSpace(coverURL), MediaURL: strings.TrimSpace(mediaURL), Available: available,
	}
}

func UnavailableVideoCard(videoID int64) *VideoCard {
	return RestoreVideoCard(videoID, 0, "", "", "", false)
}

func (m *Member) AdvanceRead(throughMessageID int64, now time.Time) bool {
	if m == nil || throughMessageID <= 0 || throughMessageID <= m.LastReadMessageID {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	m.LastReadMessageID = throughMessageID
	normalized := now.UTC()
	m.LastReadAt = &normalized
	return true
}

func (m *Member) SetUnreadCount(count int) {
	if m == nil {
		return
	}
	if count < 0 {
		count = 0
	}
	m.UnreadCount = count
}

func (c *Conversation) Contains(userID int64) bool {
	return c != nil && userID > 0 && (c.LowerUserID == userID || c.HigherUserID == userID)
}

func (c *Conversation) Counterpart(userID int64) int64 {
	if c == nil {
		return 0
	}
	if c.LowerUserID == userID {
		return c.HigherUserID
	}
	if c.HigherUserID == userID {
		return c.LowerUserID
	}
	return 0
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
