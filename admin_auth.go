package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	adminSessionCookieName = "fb2_admin_session"
	adminSessionTTL        = 24 * time.Hour
)

type adminAuth struct {
	password string

	mu       sync.Mutex
	sessions map[string]time.Time
}

func newAdminAuth(password string) *adminAuth {
	return &adminAuth{
		password: strings.TrimSpace(password),
		sessions: make(map[string]time.Time),
	}
}

func (a *adminAuth) Enabled() bool {
	return a != nil && a.password != ""
}

func (a *adminAuth) Login(password string) (string, error) {
	if !a.Enabled() {
		return "", errors.New("admin password is not configured")
	}
	if strings.TrimSpace(password) != a.password {
		return "", errors.New("invalid admin password")
	}
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(adminSessionTTL)
	a.mu.Unlock()
	return token, nil
}

func (a *adminAuth) IsAuthorized(token string) bool {
	if !a.Enabled() {
		return true
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	expiresAt, ok := a.sessions[token]
	if !ok {
		return false
	}
	if now.After(expiresAt) {
		delete(a.sessions, token)
		return false
	}
	a.sessions[token] = now.Add(adminSessionTTL)
	return true
}

func (a *adminAuth) Logout(token string) {
	if a == nil {
		return
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
}

func randomToken(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	seed := make([]byte, size)
	if _, err := rand.Read(seed); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(seed), nil
}

func isRequestSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
