package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	oidchttp "github.com/zitadel/oidc/v3/pkg/http"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"golang.org/x/oauth2"
)

type nonceContextKey struct{}

type Handler struct {
	config   Config
	party    rp.RelyingParty
	sessions sessions
}

func New(ctx context.Context, config Config) (*Handler, error) {
	return newHandler(ctx, config, &http.Client{Timeout: 10 * time.Second})
}

func newHandler(ctx context.Context, config Config, client *http.Client) (*Handler, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	config.Issuer = strings.TrimRight(config.Issuer, "/")
	h := &Handler{config: config}
	if config.Mode == "development" {
		return h, nil
	}
	block, err := aes.NewCipher(config.SessionKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	secure := strings.HasPrefix(config.BaseURL, "https://")
	cookieName := "herbstfest_session"
	if secure {
		cookieName = "__Host-herbstfest_session"
	}
	h.sessions = sessions{
		aead: aead, cookieName: cookieName, secure: secure,
		binding: []byte(config.Issuer + "\x00" + config.ClientID + "\x00" + config.ProjectID + "\x00" + config.BaseURL),
	}
	cookieOptions := []oidchttp.CookieHandlerOpt{oidchttp.WithMaxAge(300)}
	if !secure {
		cookieOptions = append(cookieOptions, oidchttp.WithUnsecure())
	}
	cookies := oidchttp.NewCookieHandler(deriveKey(config.SessionKey, "oidc-signing"), deriveKey(config.SessionKey, "oidc-encryption"), cookieOptions...)
	scopes := []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail,
		"urn:zitadel:iam:org:project:id:" + config.ProjectID + ":aud",
		"urn:zitadel:iam:org:projects:roles"}
	h.party, err = rp.NewRelyingPartyOIDC(ctx, config.Issuer, config.ClientID, "", config.BaseURL+"/auth/callback", scopes,
		rp.WithPKCE(cookies), rp.WithHTTPClient(client), rp.WithAuthStyle(oauth2.AuthStyleInParams),
		rp.WithVerifierOpts(rp.WithNonce(func(ctx context.Context) string {
			nonce, _ := ctx.Value(nonceContextKey{}).(string)
			return nonce
		})),
		rp.WithErrorHandler(func(w http.ResponseWriter, r *http.Request, kind, _, _ string) {
			slog.Warn("OIDC login rejected", "type", kind)
			http.Error(w, "Anmeldung fehlgeschlagen. Bitte versuche es erneut.", http.StatusUnauthorized)
		}),
		rp.WithUnauthorizedHandler(func(w http.ResponseWriter, r *http.Request, _, _ string) {
			slog.Warn("OIDC callback validation failed")
			http.Error(w, "Anmeldung konnte nicht bestätigt werden. Bitte versuche es erneut.", http.StatusUnauthorized)
		}))
	if err != nil {
		return nil, fmt.Errorf("initialize ZITADEL OIDC: %w", err)
	}
	return h, nil
}

func deriveKey(key []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(purpose))
	return mac.Sum(nil)
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /auth/login", noStore(http.HandlerFunc(h.login)))
	mux.Handle("GET /auth/callback", noStore(http.HandlerFunc(h.callback)))
	mux.Handle("POST /auth/logout", noStore(http.HandlerFunc(h.logout)))
}

func (h *Handler) Protect(next, denied http.Handler) http.Handler {
	return noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.config.Mode == "development" {
			user := User{Name: "Lokale Entwicklung", Development: true}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
			return
		}
		session, err := h.sessions.read(r)
		if err != nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/auth/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, session.User))
		if !session.Member {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/")
			}
			if denied != nil {
				denied.ServeHTTP(w, r)
			} else {
				http.Error(w, "Für diesen Arbeitsbereich brauchst du die HerbstfestOS-Rolle club-member.", http.StatusForbidden)
			}
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if h.party == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	state := rand.Text()
	rp.AuthURLHandler(func() string { return state }, h.party, rp.WithURLParam("nonce", state))(w, r)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	if h.party == nil {
		http.Error(w, "OIDC ist in der lokalen Entwicklung deaktiviert.", http.StatusNotFound)
		return
	}
	// The RP verifies state against its signed browser cookie before token exchange.
	// Reusing that verified random state as the nonce binds the ID token to this login.
	r = r.WithContext(context.WithValue(r.Context(), nonceContextKey{}, r.URL.Query().Get("state")))
	rp.CodeExchangeHandler[*oidc.IDTokenClaims](rp.UserinfoCallback[*oidc.IDTokenClaims, *oidc.UserInfo](h.authenticated), h.party)(w, r)
}

func (h *Handler) authenticated(w http.ResponseWriter, r *http.Request, tokens *oidc.Tokens[*oidc.IDTokenClaims], _ string, _ rp.RelyingParty, info *oidc.UserInfo) {
	expires := min(tokens.IDTokenClaims.GetExpiration().Unix(), time.Now().Add(time.Hour).Unix())
	value := session{
		User:   User{Subject: info.Subject, Name: bounded(info.Name), Email: bounded(info.Email)},
		Member: hasMemberRole(info, h.config.ProjectID), ExpiresAt: expires, IDToken: tokens.IDToken,
	}
	if expires <= time.Now().Unix() {
		http.Error(w, "Anmeldung ist bereits abgelaufen.", http.StatusUnauthorized)
		return
	}
	if err := h.sessions.write(w, value); err != nil {
		slog.Error("unable to create login session", "error", err)
		http.Error(w, "Anmeldung konnte nicht gespeichert werden.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func hasMemberRole(info *oidc.UserInfo, projectID string) bool {
	roles, ok := info.Claims["urn:zitadel:iam:org:project:"+projectID+":roles"].(map[string]any)
	if !ok {
		return false
	}
	organizations, ok := roles["club-member"].(map[string]any)
	if !ok {
		return false
	}
	for id, domain := range organizations {
		if _, ok := domain.(string); ok && id != "" {
			return true
		}
	}
	return false
}

func bounded(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > 128 {
		runes = runes[:128]
	}
	return string(runes)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.party == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	session, err := h.sessions.read(r)
	h.sessions.clear(w)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	endpoint, err := url.Parse(h.party.GetEndSessionEndpoint())
	if err != nil || endpoint.Host == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	query := endpoint.Query()
	query.Set("id_token_hint", session.IDToken)
	query.Set("client_id", h.config.ClientID)
	query.Set("post_logout_redirect_uri", h.config.BaseURL+"/")
	endpoint.RawQuery = query.Encode()
	http.Redirect(w, r, endpoint.String(), http.StatusSeeOther)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		next.ServeHTTP(w, r)
	})
}
