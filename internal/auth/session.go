package auth

import (
	"context"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const maxSessionCookieSize = 3500

type User struct {
	Subject     string `json:"sub"`
	Name        string `json:"name,omitempty"`
	Email       string `json:"email,omitempty"`
	Development bool   `json:"-"`
}

func (u User) DisplayName() string {
	if u.Name != "" {
		return u.Name
	}
	if u.Email != "" {
		return u.Email
	}
	return "Festival-Team"
}

type userContextKey struct{}

func CurrentUser(ctx context.Context) User {
	user, _ := ctx.Value(userContextKey{}).(User)
	return user
}

type session struct {
	User
	Member    bool   `json:"member"`
	ExpiresAt int64  `json:"exp"`
	IDToken   string `json:"id_token"`
}

type sessions struct {
	aead       cipher.AEAD
	cookieName string
	binding    []byte
	secure     bool
}

func (s sessions) write(w http.ResponseWriter, value session) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	// GCMWithRandomNonce prepends the nonce and authenticates the ciphertext.
	encoded := base64.RawURLEncoding.EncodeToString(s.aead.Seal(nil, nil, data, s.binding))
	if len(encoded) > maxSessionCookieSize {
		return errors.New("session exceeds browser cookie size")
	}
	cookie := s.cookie(encoded)
	cookie.Expires = time.Unix(value.ExpiresAt, 0)
	cookie.MaxAge = max(int(time.Until(cookie.Expires).Seconds()), 1)
	http.SetCookie(w, cookie)
	return nil
}

func (s sessions) read(r *http.Request) (session, error) {
	var value session
	cookie, err := r.Cookie(s.cookieName)
	if err != nil || len(cookie.Value) > maxSessionCookieSize {
		return value, errors.New("no valid session cookie")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return value, errors.New("invalid session encoding")
	}
	data, err := s.aead.Open(nil, nil, ciphertext, s.binding)
	if err != nil {
		return value, errors.New("invalid session authentication")
	}
	if err := json.Unmarshal(data, &value); err != nil || value.Subject == "" || value.ExpiresAt <= time.Now().Unix() {
		return session{}, errors.New("invalid or expired session")
	}
	return value, nil
}

func (s sessions) clear(w http.ResponseWriter) {
	cookie := s.cookie("")
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(1, 0)
	http.SetCookie(w, cookie)
}

func (s sessions) cookie(value string) *http.Cookie {
	return &http.Cookie{Name: s.cookieName, Value: value, Path: "/", Secure: s.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}
