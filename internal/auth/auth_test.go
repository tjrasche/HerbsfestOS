package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

const testClientID = "festival-client"
const testProjectID = "festival-project"

func TestConfigFailsClosed(t *testing.T) {
	values := map[string]string{
		"ZITADEL_ISSUER": "https://auth.example", "APP_BASE_URL": "https://app.example",
		"ZITADEL_CLIENT_ID": testClientID, "ZITADEL_PROJECT_ID": testProjectID,
		"AUTH_SESSION_KEY": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32))),
	}
	getenv := func(name string) string { return values[name] }
	config, err := ConfigFromEnv(getenv)
	if err != nil || config.Mode != "oidc" {
		t.Fatalf("default must require OIDC: %+v, %v", config, err)
	}
	for _, name := range []string{"ZITADEL_ISSUER", "APP_BASE_URL", "ZITADEL_CLIENT_ID", "ZITADEL_PROJECT_ID", "AUTH_SESSION_KEY"} {
		t.Run("missing "+name, func(t *testing.T) {
			old := values[name]
			values[name] = ""
			defer func() { values[name] = old }()
			if _, err := ConfigFromEnv(getenv); err == nil {
				t.Fatal("missing configuration must not permit access")
			}
		})
	}
	for _, base := range []string{"http://app.example", "https://app.example/path", "https://user:secret@app.example", "https://app.example?redirect=x"} {
		config.BaseURL = base
		if err := config.validate(); err == nil {
			t.Errorf("accepted unsafe app origin %q", base)
		}
	}
	config.BaseURL = "http://localhost:8080"
	if err := config.validate(); err != nil {
		t.Errorf("local OIDC test origin must work: %v", err)
	}
	if _, err := ConfigFromEnv(func(name string) string {
		if name == "AUTH_MODE" {
			return "development"
		}
		return ""
	}); err != nil {
		t.Fatalf("explicit local bypass must work without credentials: %v", err)
	}
}

func TestSessionRejectsTamperingExpiryAndDifferentClient(t *testing.T) {
	block, err := aes.NewCipher([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		t.Fatal(err)
	}
	store := sessions{aead: aead, cookieName: "__Host-test", binding: []byte("client-one"), secure: true}
	value := session{User: User{Subject: "user-one"}, Member: true, ExpiresAt: time.Now().Add(time.Hour).Unix(), IDToken: "test-token"}
	w := httptest.NewRecorder()
	if err := store.write(w, value); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" {
		t.Fatal("session cookie must be secure, host-only, HttpOnly and SameSite Lax")
	}
	r := httptest.NewRequest(http.MethodGet, "https://app.example/", nil)
	r.AddCookie(cookie)
	if got, err := store.read(r); err != nil || got.Subject != "user-one" || !got.Member {
		t.Fatalf("valid session failed: %+v, %v", got, err)
	}
	altered := store
	altered.binding = []byte("client-two")
	if _, err := altered.read(r); err == nil {
		t.Fatal("a session for a different client was accepted")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)/2] ^= 1
	cookie.Value = base64.RawURLEncoding.EncodeToString(ciphertext)
	r = httptest.NewRequest(http.MethodGet, "https://app.example/", nil)
	r.AddCookie(cookie)
	if _, err := store.read(r); err == nil {
		t.Fatal("tampered session was accepted")
	}
	value.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	w = httptest.NewRecorder()
	if err := store.write(w, value); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodGet, "https://app.example/", nil)
	r.AddCookie(w.Result().Cookies()[0])
	if _, err := store.read(r); err == nil {
		t.Fatal("expired session was accepted")
	}
	value.IDToken = strings.Repeat("x", 4000)
	if err := store.write(httptest.NewRecorder(), value); err == nil {
		t.Fatal("oversized cookie was accepted")
	}
}

type testProvider struct {
	server        *httptest.Server
	key           *rsa.PrivateKey
	mu            sync.Mutex
	nonce         string
	challenge     string
	tokenRequests int
}

func newTestProvider(t *testing.T) *testProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &testProvider{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize",
			"token_endpoint": p.server.URL + "/token", "userinfo_endpoint": p.server.URL + "/userinfo",
			"jwks_uri": p.server.URL + "/keys", "end_session_endpoint": p.server.URL + "/logout",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: string(jose.RS256), Use: "sig"}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		p.mu.Lock()
		p.tokenRequests++
		nonce, challenge := p.nonce, p.challenge
		p.mu.Unlock()
		if r.Form.Get("client_id") != testClientID || oidc.NewSHACodeChallenge(r.Form.Get("code_verifier")) != challenge {
			t.Error("token exchange did not use the expected client and PKCE verifier")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		code := r.Form.Get("code")
		issuer, audience, expiry := p.server.URL, testClientID, time.Now().Add(time.Hour).Unix()
		if code == "wrong-nonce" {
			nonce = "a-different-login"
		}
		if code == "wrong-issuer" {
			issuer = "https://untrusted.example"
		}
		if code == "wrong-audience" {
			audience = "another-client"
		}
		if code == "expired-token" {
			expiry = time.Now().Add(-time.Minute).Unix()
		}
		claims := map[string]any{"iss": issuer, "aud": audience, "sub": "festival-user", "iat": time.Now().Unix(), "exp": expiry, "nonce": nonce}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		signed, err := signer.Sign(payload)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		token, err := signed.CompactSerialize()
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		if code == "wrong-signature" {
			parts := strings.Split(token, ".")
			signature, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			signature[0] ^= 1
			parts[2] = base64.RawURLEncoding.EncodeToString(signature)
			token = strings.Join(parts, ".")
		}
		writeJSON(w, map[string]any{"access_token": "access." + code, "token_type": "Bearer", "expires_in": 3600, "id_token": token})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer access.")
		subject := "festival-user"
		if code == "wrong-subject" {
			subject = "another-user"
		}
		info := map[string]any{"sub": subject, "name": "Festival Team", "email": "team@example.com"}
		if code != "no-role" {
			project := testProjectID
			if code == "wrong-project" {
				project = "another-project"
			}
			info["urn:zitadel:iam:org:project:"+project+":roles"] = map[string]any{"club-member": map[string]any{"festival-org": "festival.example"}}
		}
		writeJSON(w, info)
	})
	p.server = httptest.NewTLSServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		panic(err)
	}
}

func TestOIDCLoginAndAccess(t *testing.T) {
	provider := newTestProvider(t)
	h, err := newHandler(context.Background(), Config{Mode: "oidc", Issuer: provider.server.URL, BaseURL: "https://app.example", ClientID: testClientID, ProjectID: testProjectID, SessionKey: []byte(strings.Repeat("s", 32))}, provider.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	private := http.NewServeMux()
	private.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r.Context()).Subject != "festival-user" {
			t.Error("verified identity was not attached to the request")
		}
		w.Write([]byte("private festival data"))
	})
	private.HandleFunc("POST /feedback-notes", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) })
	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", h.Protect(private, nil))
	app := http.NewCrossOriginProtection().Handler(mux)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/"
		if method == http.MethodPost {
			path = "/feedback-notes"
		}
		for _, htmx := range []bool{false, true} {
			r := httptest.NewRequest(method, "https://app.example"+path, nil)
			if htmx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			if htmx {
				if w.Code != http.StatusUnauthorized || w.Header().Get("HX-Redirect") != "/auth/login" {
					t.Fatalf("htmx must use a browser redirect: %d, %v", w.Code, w.Header())
				}
			} else if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/auth/login" {
				t.Fatalf("unauthenticated request reached app: %d", w.Code)
			}
		}
	}
	for _, code := range []string{"member", "no-role", "wrong-project", "wrong-state", "wrong-nonce", "wrong-issuer", "wrong-audience", "wrong-signature", "expired-token", "wrong-subject"} {
		t.Run(code, func(t *testing.T) {
			login := httptest.NewRecorder()
			app.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "https://app.example/auth/login", nil))
			if login.Code != http.StatusFound {
				t.Fatalf("login failed: %d, %s", login.Code, login.Body)
			}
			authorization, err := url.Parse(login.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			params := authorization.Query()
			if params.Get("code_challenge_method") != "S256" || params.Get("client_id") != testClientID || params.Get("redirect_uri") != "https://app.example/auth/callback" || params.Get("nonce") == "" {
				t.Fatalf("invalid authorization request: %v", params)
			}
			if !strings.Contains(params.Get("scope"), "urn:zitadel:iam:org:project:id:"+testProjectID+":aud") {
				t.Fatal("project audience missing")
			}
			provider.mu.Lock()
			provider.nonce, provider.challenge = params.Get("nonce"), params.Get("code_challenge")
			before := provider.tokenRequests
			provider.mu.Unlock()
			state := params.Get("state")
			if code == "wrong-state" {
				state = "state-from-a-different-login"
			}
			r := httptest.NewRequest(http.MethodGet, "https://app.example/auth/callback?"+url.Values{"state": {state}, "code": {code}}.Encode(), nil)
			for _, cookie := range login.Result().Cookies() {
				r.AddCookie(cookie)
			}
			callback := httptest.NewRecorder()
			app.ServeHTTP(callback, r)
			var sessionCookie *http.Cookie
			for _, cookie := range callback.Result().Cookies() {
				if cookie.Name == h.sessions.cookieName {
					sessionCookie = cookie
				}
			}
			if strings.HasPrefix(code, "wrong-") && code != "wrong-project" || code == "expired-token" {
				if callback.Code != http.StatusUnauthorized || sessionCookie != nil {
					t.Fatalf("invalid authentication accepted: %d, %s", callback.Code, callback.Body)
				}
				if code == "wrong-state" {
					provider.mu.Lock()
					after := provider.tokenRequests
					provider.mu.Unlock()
					if after != before {
						t.Fatal("invalid state reached token exchange")
					}
				}
				return
			}
			if callback.Code != http.StatusSeeOther || sessionCookie == nil {
				t.Fatalf("callback failed: %d, %s", callback.Code, callback.Body)
			}
			r = httptest.NewRequest(http.MethodGet, "https://app.example/", nil)
			r.AddCookie(sessionCookie)
			response := httptest.NewRecorder()
			app.ServeHTTP(response, r)
			if code != "member" {
				if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "private festival data") {
					t.Fatalf("user without project role accessed app: %d", response.Code)
				}
				return
			}
			if response.Code != http.StatusOK || response.Body.String() != "private festival data" {
				t.Fatalf("member failed to access app: %d, %s", response.Code, response.Body)
			}
			logout := httptest.NewRequest(http.MethodPost, "https://app.example/auth/logout", nil)
			logout.AddCookie(sessionCookie)
			logout.Header.Set("Sec-Fetch-Site", "cross-site")
			response = httptest.NewRecorder()
			app.ServeHTTP(response, logout)
			if response.Code != http.StatusForbidden || len(response.Result().Cookies()) != 0 {
				t.Fatal("cross-site logout changed session")
			}
			logout.Header.Set("Sec-Fetch-Site", "same-origin")
			response = httptest.NewRecorder()
			app.ServeHTTP(response, logout)
			endSession, err := url.Parse(response.Header().Get("Location"))
			if err != nil || response.Code != http.StatusSeeOther || endSession.Host != strings.TrimPrefix(provider.server.URL, "https://") || endSession.Query().Get("post_logout_redirect_uri") != "https://app.example/" {
				t.Fatalf("logout did not reach provider: %d, %s", response.Code, response.Body)
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
				t.Fatal("logout did not clear local session")
			}
		})
	}
}

func TestDevelopmentBypassIsExplicit(t *testing.T) {
	h, err := New(context.Background(), Config{Mode: "development"})
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r.Context())
		if !user.Development || user.Subject != "" {
			t.Fatal("local development must be visibly distinct from a verified identity")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	h.Protect(next, nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("explicit local development failed: %d", w.Code)
	}
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("missing configuration must not default to bypass")
	}
}

func TestProjectRoleClaims(t *testing.T) {
	claim := "urn:zitadel:iam:org:project:" + testProjectID + ":roles"
	for _, roles := range []any{nil, "club-member", []string{"club-member"},
		map[string]any{"club-member": true},
		map[string]any{"club-member": map[string]any{}},
		map[string]any{"club-member": map[string]any{"": "festival.example"}},
		map[string]any{"member": map[string]any{"festival-org": "festival.example"}}} {
		info := &oidc.UserInfo{Claims: map[string]any{claim: roles}}
		if hasMemberRole(info, testProjectID) {
			t.Fatalf("invalid role claim permitted access: %#v", roles)
		}
	}
}
