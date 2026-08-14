package infrajwt

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const (
	defaultAccessTTL      = 5 * time.Minute
	defaultAdminAccessTTL = 30 * time.Minute
	defaultClockLeeway    = 30 * time.Second
	maxAccessTTL          = 15 * time.Minute
	maxAdminAccessTTL     = 8 * time.Hour
	maxClockLeeway        = 2 * time.Minute
	TokenTypeAccess       = "access"
	TokenTypeAdminAccess  = "admin_access"
	AudienceConsumer      = "frux-consumer"
	AudienceAdmin         = "frux-admin"
	DefaultIssuer         = "frux"
)

var ErrEmptyJWTSecret = errors.New("jwt secret is required")
var ErrParseAccessTTL = errors.New("parse jwt access_ttl failed")
var ErrParseAdminAccessTTL = errors.New("parse jwt admin_access_ttl failed")
var ErrEmptyToken = errors.New("token is empty")
var ErrParseJWTToken = errors.New("parse jwt token failed")
var ErrInvalidTokenType = errors.New("token type invalid")
var ErrInvalidTokenUserID = errors.New("token user id invalid")
var ErrInvalidUserID = errors.New("user id must be positive")
var ErrGenerateTokenJTI = errors.New("generate token jti failed")
var ErrSignJWTToken = errors.New("sign jwt token failed")
var ErrInvalidTTL = errors.New("ttl must be positive")
var ErrInvalidKeyRing = errors.New("jwt key ring is invalid")
var ErrUnknownKeyID = errors.New("jwt key id is unknown")
var ErrInvalidClaims = errors.New("jwt claims are invalid")

type KeyRingConfig struct {
	ActiveKeyID string
	Keys        map[string]string
}

type ManagerConfig struct {
	Issuer            string
	AccessTTL         time.Duration
	AdminAccessTTL    time.Duration
	ClockLeeway       time.Duration
	Consumer          KeyRingConfig
	Admin             KeyRingConfig
	LegacySecret      string
	LegacyAcceptUntil time.Time
}

// Claims 是业务侧读取到的 token 信息，避免 HTTP 层直接依赖第三方 JWT 结构。
type Claims struct {
	UserID      int64  `json:"uid"`
	SessionID   string `json:"sid"`
	AuthVersion int64  `json:"ver"`
	Role        string `json:"role"`
	TokenType   string `json:"token_type"`
	JWTID       string `json:"jti"`
	IssuedAt    int64  `json:"iat"`
	NotBefore   int64  `json:"nbf"`
	ExpiresAt   int64  `json:"exp"`
	Audience    string `json:"aud"`
	Issuer      string `json:"iss"`
	KeyID       string `json:"kid"`
	Legacy      bool   `json:"legacy"`
}

type keyRing struct {
	activeKeyID string
	keys        map[string][]byte
}

// Manager 统一负责 JWT 签发和校验。
type Manager struct {
	issuer            string
	consumer          keyRing
	admin             keyRing
	legacySecret      []byte
	legacyAcceptUntil time.Time
	clockLeeway       time.Duration
	accessTTL         time.Duration
	adminAccessTTL    time.Duration

	// secret 保留给迁移测试构造旧 Token，业务签发不再使用它。
	secret []byte
}

// tokenClaims 同时保留旧 uid/role 字段，以便在迁移窗口解析历史 Token。
type tokenClaims struct {
	UserID      int64  `json:"uid,omitempty"`
	SessionID   string `json:"sid,omitempty"`
	AuthVersion int64  `json:"ver,omitempty"`
	Role        string `json:"role,omitempty"`
	TokenType   string `json:"token_type"`
	jwtlib.RegisteredClaims
}

// NewManager 是兼容测试和旧装配代码的构造器。新装配应使用 NewManagerWithConfig。
func NewManager(secret, accessTTL string, adminAccessTTL ...string) (*Manager, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, ErrEmptyJWTSecret
	}
	accessDuration, err := parseTTL(accessTTL, 15*time.Minute)
	if err != nil || accessDuration > maxAccessTTL {
		return nil, ErrParseAccessTTL
	}
	rawAdminTTL := ""
	if len(adminAccessTTL) > 0 {
		rawAdminTTL = adminAccessTTL[0]
	}
	adminDuration, err := parseTTL(rawAdminTTL, defaultAdminAccessTTL)
	if err != nil || adminDuration > maxAdminAccessTTL {
		return nil, ErrParseAdminAccessTTL
	}
	legacyUntil := time.Now().Add(maxDuration(accessDuration, adminDuration))
	return NewManagerWithConfig(ManagerConfig{
		Issuer:         DefaultIssuer,
		AccessTTL:      accessDuration,
		AdminAccessTTL: adminDuration,
		ClockLeeway:    defaultClockLeeway,
		Consumer: KeyRingConfig{
			ActiveKeyID: "consumer-v1",
			Keys:        map[string]string{"consumer-v1": secret + ":consumer"},
		},
		Admin: KeyRingConfig{
			ActiveKeyID: "admin-v1",
			Keys:        map[string]string{"admin-v1": secret + ":admin"},
		},
		LegacySecret:      secret,
		LegacyAcceptUntil: legacyUntil,
	})
}

func NewManagerWithConfig(config ManagerConfig) (*Manager, error) {
	config.Issuer = strings.TrimSpace(config.Issuer)
	if config.Issuer == "" {
		config.Issuer = DefaultIssuer
	}
	if config.AccessTTL == 0 {
		config.AccessTTL = defaultAccessTTL
	}
	if config.AccessTTL <= 0 || config.AccessTTL > maxAccessTTL {
		return nil, ErrParseAccessTTL
	}
	if config.AdminAccessTTL == 0 {
		config.AdminAccessTTL = defaultAdminAccessTTL
	}
	if config.AdminAccessTTL < 5*time.Minute ||
		config.AdminAccessTTL > maxAdminAccessTTL {
		return nil, ErrParseAdminAccessTTL
	}
	if config.ClockLeeway == 0 {
		config.ClockLeeway = defaultClockLeeway
	}
	if config.ClockLeeway < 0 || config.ClockLeeway > maxClockLeeway {
		return nil, ErrInvalidTTL
	}
	consumer, err := newKeyRing(config.Consumer)
	if err != nil {
		return nil, err
	}
	admin, err := newKeyRing(config.Admin)
	if err != nil {
		return nil, err
	}
	legacySecret := strings.TrimSpace(config.LegacySecret)
	if legacySecret != "" && config.LegacyAcceptUntil.IsZero() {
		return nil, ErrInvalidKeyRing
	}
	manager := &Manager{
		issuer:            config.Issuer,
		consumer:          consumer,
		admin:             admin,
		legacySecret:      []byte(legacySecret),
		legacyAcceptUntil: config.LegacyAcceptUntil,
		clockLeeway:       config.ClockLeeway,
		accessTTL:         config.AccessTTL,
		adminAccessTTL:    config.AdminAccessTTL,
		secret:            []byte(legacySecret),
	}
	return manager, nil
}

func newKeyRing(config KeyRingConfig) (keyRing, error) {
	active := strings.TrimSpace(config.ActiveKeyID)
	if active == "" || len(config.Keys) == 0 {
		return keyRing{}, ErrInvalidKeyRing
	}
	keys := make(map[string][]byte, len(config.Keys))
	for rawID, rawSecret := range config.Keys {
		id := strings.TrimSpace(rawID)
		secret := strings.TrimSpace(rawSecret)
		if id == "" || secret == "" {
			return keyRing{}, ErrInvalidKeyRing
		}
		if _, exists := keys[id]; exists {
			return keyRing{}, ErrInvalidKeyRing
		}
		keys[id] = []byte(secret)
	}
	if _, exists := keys[active]; !exists {
		return keyRing{}, ErrInvalidKeyRing
	}
	return keyRing{activeKeyID: active, keys: keys}, nil
}

func (m *Manager) AccessTTL() time.Duration {
	return m.accessTTL
}

func (m *Manager) AdminAccessTTL() time.Duration {
	return m.adminAccessTTL
}

// SignAccessToken 保留旧调用形状；新业务使用 SignConsumerAccessToken。
func (m *Manager) SignAccessToken(userID int64, _ string) (string, error) {
	sessionID, err := randomID(16)
	if err != nil {
		return "", ErrGenerateTokenJTI
	}
	return m.SignConsumerAccessToken(userID, sessionID, 1)
}

func (m *Manager) SignConsumerAccessToken(
	userID int64,
	sessionID string,
	authVersion int64,
) (string, error) {
	return m.signToken(
		userID, strings.TrimSpace(sessionID), authVersion, TokenTypeAccess,
		AudienceConsumer, m.accessTTL, m.consumer,
	)
}

// SignAdminAccessToken 保留旧调用形状；新业务使用 SignAdminAccessTokenVersion。
func (m *Manager) SignAdminAccessToken(userID int64, _ string) (string, error) {
	return m.SignAdminAccessTokenVersion(userID, 1)
}

func (m *Manager) SignAdminAccessTokenVersion(
	userID int64,
	authVersion int64,
) (string, error) {
	return m.signToken(
		userID, "", authVersion, TokenTypeAdminAccess, AudienceAdmin,
		m.adminAccessTTL, m.admin,
	)
}

func (m *Manager) ParseAndValidateToken(
	token, expectedType, expectedAudience string,
) (*Claims, error) {
	var ring keyRing
	switch expectedType {
	case TokenTypeAccess:
		ring = m.consumer
	case TokenTypeAdminAccess:
		ring = m.admin
	default:
		return nil, ErrInvalidTokenType
	}
	claims, err := m.parseStrict(token, expectedType, expectedAudience, ring)
	if err == nil {
		return claims, nil
	}
	return m.parseLegacy(token, expectedType, expectedAudience)
}

func (m *Manager) ParseAndValidateConsumerToken(token string) (*Claims, error) {
	return m.ParseAndValidateToken(token, TokenTypeAccess, AudienceConsumer)
}

func (m *Manager) ParseAndValidateAdminToken(token string) (*Claims, error) {
	return m.ParseAndValidateToken(token, TokenTypeAdminAccess, AudienceAdmin)
}

func (m *Manager) parseStrict(
	token, expectedType, expectedAudience string,
	ring keyRing,
) (*Claims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrEmptyToken
	}
	parsedClaims := &tokenClaims{}
	parsed, err := jwtlib.ParseWithClaims(
		token,
		parsedClaims,
		func(token *jwtlib.Token) (any, error) {
			kid, _ := token.Header["kid"].(string)
			key, exists := ring.keys[strings.TrimSpace(kid)]
			if !exists {
				return nil, ErrUnknownKeyID
			}
			return key, nil
		},
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithExpirationRequired(),
		jwtlib.WithIssuedAt(),
		jwtlib.WithLeeway(m.clockLeeway),
		jwtlib.WithIssuer(m.issuer),
		jwtlib.WithAudience(expectedAudience),
	)
	if err != nil || parsed == nil || !parsed.Valid {
		return nil, ErrParseJWTToken
	}
	keyID, _ := parsed.Header["kid"].(string)
	if parsedClaims.TokenType != expectedType {
		return nil, ErrInvalidTokenType
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(parsedClaims.Subject), 10, 64)
	if err != nil || userID <= 0 {
		return nil, ErrInvalidTokenUserID
	}
	if parsedClaims.AuthVersion <= 0 || strings.TrimSpace(parsedClaims.ID) == "" ||
		parsedClaims.IssuedAt == nil || parsedClaims.NotBefore == nil ||
		parsedClaims.ExpiresAt == nil {
		return nil, ErrInvalidClaims
	}
	sessionID := strings.TrimSpace(parsedClaims.SessionID)
	if expectedType == TokenTypeAccess && sessionID == "" {
		return nil, ErrInvalidClaims
	}
	return &Claims{
		UserID:      userID,
		SessionID:   sessionID,
		AuthVersion: parsedClaims.AuthVersion,
		TokenType:   parsedClaims.TokenType,
		JWTID:       parsedClaims.ID,
		IssuedAt:    claimTimeUnix(parsedClaims.IssuedAt),
		NotBefore:   claimTimeUnix(parsedClaims.NotBefore),
		ExpiresAt:   claimTimeUnix(parsedClaims.ExpiresAt),
		Audience:    expectedAudience,
		Issuer:      parsedClaims.Issuer,
		KeyID:       strings.TrimSpace(keyID),
	}, nil
}

func (m *Manager) parseLegacy(
	token, expectedType, expectedAudience string,
) (*Claims, error) {
	if len(m.legacySecret) == 0 || m.legacyAcceptUntil.IsZero() ||
		!time.Now().Before(m.legacyAcceptUntil) {
		return nil, ErrParseJWTToken
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrEmptyToken
	}
	parsedClaims := &tokenClaims{}
	parsed, err := jwtlib.ParseWithClaims(
		token,
		parsedClaims,
		func(*jwtlib.Token) (any, error) {
			return m.legacySecret, nil
		},
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithExpirationRequired(),
		jwtlib.WithLeeway(m.clockLeeway),
	)
	if err != nil || parsed == nil || !parsed.Valid {
		return nil, ErrParseJWTToken
	}
	if parsedClaims.TokenType != expectedType {
		return nil, ErrInvalidTokenType
	}
	if parsedClaims.UserID <= 0 {
		return nil, ErrInvalidTokenUserID
	}
	audience := ""
	if len(parsedClaims.Audience) > 0 {
		audience = parsedClaims.Audience[0]
		if audience != expectedAudience {
			return nil, ErrParseJWTToken
		}
	}
	authVersion := parsedClaims.AuthVersion
	if authVersion <= 0 {
		authVersion = 1
	}
	return &Claims{
		UserID:      parsedClaims.UserID,
		AuthVersion: authVersion,
		Role:        parsedClaims.Role,
		TokenType:   parsedClaims.TokenType,
		JWTID:       parsedClaims.ID,
		IssuedAt:    claimTimeUnix(parsedClaims.IssuedAt),
		NotBefore:   claimTimeUnix(parsedClaims.NotBefore),
		ExpiresAt:   claimTimeUnix(parsedClaims.ExpiresAt),
		Audience:    audience,
		Issuer:      parsedClaims.Issuer,
		Legacy:      true,
	}, nil
}

func (m *Manager) signToken(
	userID int64,
	sessionID string,
	authVersion int64,
	tokenType, audience string,
	ttl time.Duration,
	ring keyRing,
) (string, error) {
	if userID <= 0 {
		return "", ErrInvalidUserID
	}
	if authVersion <= 0 || (tokenType == TokenTypeAccess && sessionID == "") {
		return "", ErrInvalidClaims
	}
	now := time.Now()
	jti, err := randomID(16)
	if err != nil {
		return "", ErrGenerateTokenJTI
	}
	claims := tokenClaims{
		SessionID:   sessionID,
		AuthVersion: authVersion,
		TokenType:   tokenType,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   strconv.FormatInt(userID, 10),
			Audience:  jwtlib.ClaimStrings{audience},
			IssuedAt:  jwtlib.NewNumericDate(now),
			NotBefore: jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(ttl)),
			ID:        jti,
		},
	}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	token.Header["kid"] = ring.activeKeyID
	signedToken, err := token.SignedString(ring.keys[ring.activeKeyID])
	if err != nil {
		return "", ErrSignJWTToken
	}
	return signedToken, nil
}

func parseTTL(raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if ttl <= 0 {
		return 0, ErrInvalidTTL
	}
	return ttl, nil
}

func randomID(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func claimTimeUnix(value *jwtlib.NumericDate) int64 {
	if value == nil {
		return 0
	}
	return value.Unix()
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}
