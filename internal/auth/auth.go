package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/config"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
	"golang.org/x/crypto/bcrypt"
)

type Role string

const (
	RoleManager Role = "MANAGER"
	RoleKitchen Role = "KITCHEN"
	RolePOS     Role = "POS"
	RoleAdmin   Role = "ADMIN"
)

type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     Role   `json:"role"`
	ShopID   string `json:"shop_id"`
	IsActive bool   `json:"is_active"`
}

type Claims struct {
	Sub    string `json:"sub"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	ShopID string `json:"shop_id"`
	jwt.RegisteredClaims
}

type Service struct {
	pool   *pgxpool.Pool
	cfg    *config.Config
	secret []byte
}

func NewService(pool *pgxpool.Pool, cfg *config.Config) *Service {
	return &Service{
		pool:   pool,
		cfg:    cfg,
		secret: []byte(cfg.JWT.Secret),
	}
}

type LoginResult struct {
	AccessToken string                 `json:"access_token"`
	User        map[string]interface{} `json:"user"`
}

func (s *Service) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	user, err := s.findByEmail(ctx, email)
	if err != nil {
		return nil, apperrors.Unauthorized("Invalid credentials")
	}
	if !user.IsActive {
		return nil, apperrors.Unauthorized("Invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.password), []byte(password)); err != nil {
		return nil, apperrors.Unauthorized("Invalid credentials")
	}

	token, err := s.signToken(user)
	if err != nil {
		return nil, apperrors.Internal("failed to issue token")
	}

	shopID := user.ShopID
	if shopID == "" {
		shopID = "1"
	}

	return &LoginResult{
		AccessToken: token,
		User: map[string]interface{}{
			"id":      user.ID,
			"email":   user.Email,
			"name":    user.Name,
			"role":    string(user.Role),
			"shopId":  shopID,
			"shop_id": shopID,
		},
	}, nil
}

type RegisterInput struct {
	Email    string
	Password string
	Name     string
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (*LoginResult, error) {
	existing, err := s.findByEmail(ctx, in.Email)
	if err == nil && existing != nil {
		return nil, apperrors.BadRequest("Email already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), 10)
	if err != nil {
		return nil, apperrors.Internal("failed to hash password")
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	shopID := "1"

	const q = `
		INSERT INTO users (id, email, password, name, role, shop_id, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, $7, $8)
		RETURNING id, email, name, role, shop_id, is_active
	`
	var u User
	var roleStr string
	err = s.pool.QueryRow(ctx, q,
		id, normalizeEmail(in.Email), string(hash), in.Name, string(RoleKitchen), shopID, now, now,
	).Scan(&u.ID, &u.Email, &u.Name, &roleStr, &u.ShopID, &u.IsActive)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	u.Role = Role(roleStr)

	token, err := s.signToken(&userRow{User: u, password: ""})
	if err != nil {
		return nil, apperrors.Internal("failed to issue token")
	}

	return &LoginResult{
		AccessToken: token,
		User: map[string]interface{}{
			"id":      u.ID,
			"email":   u.Email,
			"name":    u.Name,
			"role":    string(u.Role),
			"shopId":  u.ShopID,
			"shop_id": u.ShopID,
		},
	}, nil
}

func (s *Service) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, apperrors.Unauthorized("Invalid or expired token")
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, apperrors.Unauthorized("Invalid or expired token")
	}
	return claims, nil
}

func (s *Service) GetUserByID(ctx context.Context, id string) (*User, error) {
	const q = `
		SELECT id, email, name, role, shop_id, is_active
		FROM users
		WHERE id = $1
	`
	var u User
	var roleStr string
	err := s.pool.QueryRow(ctx, q, id).Scan(&u.ID, &u.Email, &u.Name, &roleStr, &u.ShopID, &u.IsActive)
	if err != nil {
		return nil, apperrors.NotFound("User not found")
	}
	u.Role = Role(roleStr)
	return &u, nil
}

type userRow struct {
	User
	password string
}

func (s *Service) findByEmail(ctx context.Context, email string) (*userRow, error) {
	const q = `
		SELECT id, email, password, name, role, shop_id, is_active
		FROM users
		WHERE email = $1
	`
	var u userRow
	var roleStr string
	err := s.pool.QueryRow(ctx, q, normalizeEmail(email)).Scan(
		&u.ID, &u.Email, &u.password, &u.Name, &roleStr, &u.ShopID, &u.IsActive,
	)
	if err != nil {
		return nil, err
	}
	u.Role = Role(roleStr)
	return &u, nil
}

func (s *Service) signToken(u *userRow) (string, error) {
	shopID := u.ShopID
	if shopID == "" {
		shopID = "1"
	}
	claims := Claims{
		Sub:    u.ID,
		Email:  u.Email,
		Role:   string(u.Role),
		ShopID: shopID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.cfg.JWT.ExpiresIn)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   u.ID,
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(s.secret)
}

func normalizeEmail(e string) string {
	// lower + trim
	b := make([]byte, 0, len(e))
	for i := 0; i < len(e); i++ {
		c := e[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != ' ' && c != '\t' {
			b = append(b, c)
		}
	}
	return string(b)
}

// Context keys for authenticated principal
type ctxKey string

const (
	CtxUserID ctxKey = "auth_user_id"
	CtxRole   ctxKey = "auth_role"
	CtxShopID ctxKey = "auth_shop_id"
	CtxEmail  ctxKey = "auth_email"
)

func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(CtxUserID).(string); ok {
		return v
	}
	return ""
}

func RoleFromContext(ctx context.Context) Role {
	if v, ok := ctx.Value(CtxRole).(string); ok {
		return Role(v)
	}
	return ""
}

func ShopIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(CtxShopID).(string); ok {
		return v
	}
	return "1"
}

// RequireShopID returns JWT shop or error — never silently falls back to "1".
func RequireShopID(ctx context.Context) (string, error) {
	shopID := ShopIDFromContext(ctx)
	if shopID == "" {
		return "", apperrors.Forbidden("shop identity required")
	}
	return shopID, nil
}

// ResolveShopID prefers JWT shop; only allows explicit fallback for non-auth paths.
func ResolveShopID(ctx context.Context, clientHint string) string {
	if s := ShopIDFromContext(ctx); s != "" {
		return s
	}
	if clientHint != "" {
		return clientHint
	}
	return ""
}

func WithPrincipal(ctx context.Context, claims *Claims) context.Context {
	ctx = context.WithValue(ctx, CtxUserID, claims.Sub)
	ctx = context.WithValue(ctx, CtxRole, claims.Role)
	ctx = context.WithValue(ctx, CtxShopID, claims.ShopID)
	ctx = context.WithValue(ctx, CtxEmail, claims.Email)
	return ctx
}
