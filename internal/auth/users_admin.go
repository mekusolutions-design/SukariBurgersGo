package auth

import (
	"context"
	"time"

	apperrors "github.com/restflow/api-go/pkg/errors"
)

type PublicUser struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	ShopID    string    `json:"shop_id"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) ListUsers(ctx context.Context, shopID string) ([]PublicUser, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, email, name, role, shop_id, is_active, created_at
		FROM users WHERE shop_id = $1 ORDER BY created_at DESC`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublicUser
	for rows.Next() {
		var u PublicUser
		var role string
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &role, &u.ShopID, &u.IsActive, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Role = Role(role)
		out = append(out, u)
	}
	if out == nil {
		out = []PublicUser{}
	}
	return out, nil
}

func (s *Service) GetUser(ctx context.Context, shopID, userID string) (*PublicUser, error) {
	var u PublicUser
	var role string
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, name, role, shop_id, is_active, created_at
		FROM users WHERE id = $1 AND shop_id = $2`, userID, shopID).
		Scan(&u.ID, &u.Email, &u.Name, &role, &u.ShopID, &u.IsActive, &u.CreatedAt)
	if err != nil {
		return nil, apperrors.NotFound("User not found")
	}
	u.Role = Role(role)
	return &u, nil
}

func (s *Service) UpdateRole(ctx context.Context, shopID, userID, newRole string) (*PublicUser, error) {
	switch Role(newRole) {
	case RoleAdmin, RoleManager, RoleKitchen, RolePOS:
	default:
		return nil, apperrors.BadRequest("invalid role")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET role = $1, updated_at = $2 WHERE id = $3 AND shop_id = $4`,
		newRole, time.Now().UTC(), userID, shopID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, apperrors.NotFound("User not found")
	}
	return s.GetUser(ctx, shopID, userID)
}
