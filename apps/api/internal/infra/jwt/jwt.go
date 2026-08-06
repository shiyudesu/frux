package infrajwt

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const (
	defaultAccessTTL      = 15 * time.Minute
	defaultAdminAccessTTL = 30 * time.Minute
	maxAdminAccessTTL     = 8 * time.Hour
	TokenTypeAccess       = "access"
	TokenTypeAdminAccess  = "admin_access"
	AudienceConsumer      = "frux-consumer"
	AudienceAdmin         = "frux-admin"
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

// Claims 是业务侧读取到的 token 信息，避免 HTTP 层直接依赖第三方 JWT 结构。
type Claims struct {
	UserID    int64  `json:"uid"`
	Role      string `json:"role"`
	TokenType string `json:"token_type"`
	JWTID     string `json:"jti"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Audience  string `json:"aud"`
}

// Manager 统一负责 JWT 签发和校验，secret 与过期时间从配置加载。
type Manager struct {
	secret         []byte
	accessTTL      time.Duration
	adminAccessTTL time.Duration
}

// tokenClaims 是真正写入 JWT 的声明，嵌入 RegisteredClaims 获得 exp、iat、jti 等标准字段。
type tokenClaims struct {
	UserID    int64  `json:"uid"`
	Role      string `json:"role"`
	TokenType string `json:"token_type"`
	jwtlib.RegisteredClaims
}

// NewManager 初始化 JWT 管理器，并解析 access token 的有效期。
func NewManager(secret, accessTTL string, adminAccessTTL ...string) (*Manager, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, ErrEmptyJWTSecret
	}

	accessDuration, err := parseTTL(accessTTL, defaultAccessTTL)
	if err != nil {
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

	return &Manager{
		secret: []byte(secret), accessTTL: accessDuration, adminAccessTTL: adminDuration,
	}, nil
}

// AccessTTL 返回访问 token 有效期，登录响应会把它换算成秒返回给客户端。
func (m *Manager) AccessTTL() time.Duration {
	return m.accessTTL
}

func (m *Manager) AdminAccessTTL() time.Duration {
	return m.adminAccessTTL
}

// SignAccessToken 签发访问 token，当前系统使用 HS256 对称签名。
func (m *Manager) SignAccessToken(userID int64, role string) (string, error) {
	return m.signToken(userID, role, TokenTypeAccess, AudienceConsumer, m.accessTTL)
}

func (m *Manager) SignAdminAccessToken(userID int64, role string) (string, error) {
	return m.signToken(userID, role, TokenTypeAdminAccess, AudienceAdmin, m.adminAccessTTL)
}

// ParseAndValidateToken 解析 token，并校验签名算法、过期时间、token 类型和 audience。
func (m *Manager) ParseAndValidateToken(token, expectedType, expectedAudience string) (*Claims, error) {
	return m.parseAndValidateToken(token, expectedType, expectedAudience, false)
}

func (m *Manager) ParseAndValidateConsumerToken(token string) (*Claims, error) {
	claims, err := m.parseAndValidateToken(
		token, TokenTypeAccess, AudienceConsumer, false,
	)
	if err == nil {
		return claims, nil
	}
	return m.parseAndValidateToken(token, TokenTypeAccess, "", true)
}

func (m *Manager) parseAndValidateToken(
	token, expectedType, expectedAudience string,
	requireMissingAudience bool,
) (*Claims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrEmptyToken
	}

	parsedClaims := &tokenClaims{}
	options := []jwtlib.ParserOption{
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithExpirationRequired(),
	}
	if expectedAudience != "" {
		options = append(options, jwtlib.WithAudience(expectedAudience))
	}
	_, err := jwtlib.ParseWithClaims(
		token,
		parsedClaims,
		func(token *jwtlib.Token) (any, error) {
			return m.secret, nil
		},
		options...,
	)
	if err != nil {
		return nil, ErrParseJWTToken
	}

	if parsedClaims.TokenType != expectedType {
		return nil, ErrInvalidTokenType
	}
	if parsedClaims.UserID <= 0 {
		return nil, ErrInvalidTokenUserID
	}
	if requireMissingAudience && len(parsedClaims.Audience) != 0 {
		return nil, ErrParseJWTToken
	}
	audience := ""
	if len(parsedClaims.Audience) > 0 {
		audience = parsedClaims.Audience[0]
	}

	return &Claims{
		UserID:    parsedClaims.UserID,
		Role:      parsedClaims.Role,
		TokenType: parsedClaims.TokenType,
		JWTID:     parsedClaims.ID,
		IssuedAt:  claimTimeUnix(parsedClaims.IssuedAt),
		ExpiresAt: claimTimeUnix(parsedClaims.ExpiresAt),
		Audience:  audience,
	}, nil
}

// signToken 组装标准声明和业务声明，并用密钥完成签名。
func (m *Manager) signToken(userID int64, role, tokenType, audience string, ttl time.Duration) (string, error) {
	if userID <= 0 {
		return "", ErrInvalidUserID
	}

	role = strings.TrimSpace(role)
	if role == "" {
		role = "user"
	}

	now := time.Now()
	// jti 是 token 唯一标识，后续可以扩展为黑名单或审计日志依据。
	jti, err := randomID(16)
	if err != nil {
		return "", ErrGenerateTokenJTI
	}
	claims := tokenClaims{
		UserID:    userID,
		Role:      role,
		TokenType: tokenType,
		RegisteredClaims: jwtlib.RegisteredClaims{
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(ttl)),
			ID:        jti,
			Audience:  jwtlib.ClaimStrings{audience},
		},
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(m.secret)
	if err != nil {
		return "", ErrSignJWTToken
	}

	return signedToken, nil
}

// parseTTL 解析配置里的时间字符串，例如 15m、1h。
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

// randomID 生成十六进制随机串，用作 JWT ID。
func randomID(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// claimTimeUnix 将 jwt.NumericDate 转成 Unix 秒，空值用 0 表示。
func claimTimeUnix(value *jwtlib.NumericDate) int64 {
	if value == nil {
		return 0
	}
	return value.Unix()
}
