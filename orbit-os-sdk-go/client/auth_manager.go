package client

import (
	"context"
	"fmt"
	"time"

	authv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/auth_service/v26"
)

// AuthManager wraps AuthService — local device authentication via Gravity.
type AuthManager struct {
	client authv26.AuthServiceClient
	ctx    context.Context
}

func NewAuthManager(client authv26.AuthServiceClient, ctx context.Context) *AuthManager {
	return &AuthManager{client: client, ctx: ctx}
}

// Login authenticates with the given username and password.
// Returns the session token and its expiry (Unix timestamp), or an error.
func (m *AuthManager) Login(username, password string) (token string, expiresAt int64, err error) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	resp, err := m.client.Login(ctx, &authv26.LoginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		return "", 0, err
	}
	if e := resp.GetError(); e != nil && e.GetMessage() != "" {
		return "", 0, fmt.Errorf("%s", e.GetMessage())
	}
	return resp.GetToken(), resp.GetExpiresAt(), nil
}

// Logout invalidates the given session token.
func (m *AuthManager) Logout(token string) error {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	resp, err := m.client.Logout(ctx, &authv26.LogoutRequest{Token: token})
	if err != nil {
		return err
	}
	if e := resp.GetError(); e != nil && e.GetMessage() != "" {
		return fmt.Errorf("%s", e.GetMessage())
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("logout failed")
	}
	return nil
}
