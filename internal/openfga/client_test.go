package openfga

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const storeID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
const modelID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantError bool
	}{
		{name: "optional connection"},
		{name: "connection before model", env: map[string]string{"OPENFGA_API_URL": "http://openfga:8080"}},
		{name: "complete", env: map[string]string{"OPENFGA_API_URL": "https://fga.example", "OPENFGA_STORE_ID": storeID, "OPENFGA_AUTHORIZATION_MODEL_ID": modelID}},
		{name: "credentials without origin", env: map[string]string{"OPENFGA_API_TOKEN": "secret"}, wantError: true},
		{name: "URL credentials", env: map[string]string{"OPENFGA_API_URL": "https://user:secret@fga.example"}, wantError: true},
		{name: "URL query", env: map[string]string{"OPENFGA_API_URL": "https://fga.example?token=secret"}, wantError: true},
		{name: "URL path", env: map[string]string{"OPENFGA_API_URL": "https://fga.example/api"}, wantError: true},
		{name: "URL scheme", env: map[string]string{"OPENFGA_API_URL": "ftp://fga.example"}, wantError: true},
		{name: "missing model", env: map[string]string{"OPENFGA_API_URL": "http://openfga:8080", "OPENFGA_STORE_ID": storeID}, wantError: true},
		{name: "invalid IDs", env: map[string]string{"OPENFGA_API_URL": "http://openfga:8080", "OPENFGA_STORE_ID": "../secret", "OPENFGA_AUTHORIZATION_MODEL_ID": modelID}, wantError: true},
		{name: "invalid duration", env: map[string]string{"OPENFGA_TIMEOUT": "secret"}, wantError: true},
		{name: "zero duration", env: map[string]string{"OPENFGA_TIMEOUT": "0s"}, wantError: true},
		{name: "excess duration", env: map[string]string{"OPENFGA_TIMEOUT": "31s"}, wantError: true},
		{name: "header injection", env: map[string]string{"OPENFGA_API_URL": "http://openfga:8080", "OPENFGA_API_TOKEN": "secret\r\n"}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ConfigFromEnv(func(key string) string { return tt.env[key] })
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v, wantError=%v", err, tt.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("configuration error leaked input: %v", err)
			}
			if err == nil && cfg.Timeout != 2*time.Second {
				t.Errorf("unexpected default timeout %v", cfg.Timeout)
			}
		})
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc, timeout time.Duration, model bool) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := Config{APIURL: server.URL, APIToken: "private-token", Timeout: timeout}
	if model {
		cfg.StoreID, cfg.AuthorizationModelID = storeID, modelID
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestCheckRequestAndDecisions(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "allowed", false: "denied"}[allowed], func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/stores/"+storeID+"/check" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing authentication/content type")
				}
				var input struct {
					ModelID string            `json:"authorization_model_id"`
					Tuple   map[string]string `json:"tuple_key"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				if input.ModelID != modelID || input.Tuple["user"] != "user:alice" || input.Tuple["relation"] != "reader" || input.Tuple["object"] != "document:1" {
					t.Errorf("wrong check payload: %+v", input)
				}
				json.NewEncoder(w).Encode(map[string]bool{"allowed": allowed})
			}, time.Second, true)
			got, err := client.Check(context.Background(), "user:alice", "reader", "document:1")
			if err != nil || got != allowed {
				t.Fatalf("got (%v,%v), want (%v,nil)", got, err, allowed)
			}
		})
	}
}

func TestCheckFailuresNeverGrant(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"unauthorized", 401, "private-token", ErrUnauthorized},
		{"forbidden", 403, "private-token", ErrUnauthorized},
		{"invalid model", 400, "private-token", ErrInvalidRequest},
		{"missing store", 404, "private-token", ErrInvalidRequest},
		{"throttled", 429, "private-token", ErrUnavailable},
		{"unavailable", 503, "private-token", ErrUnavailable},
		{"missing decision", 200, `{}`, ErrInvalidResponse},
		{"null decision", 200, `{"allowed":null}`, ErrInvalidResponse},
		{"invalid JSON", 200, `private-token`, ErrInvalidResponse},
		{"trailing JSON", 200, `{"allowed":true}{}`, ErrInvalidResponse},
		{"oversized response", 200, strings.Repeat(" ", 65537), ErrInvalidResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); w.Write([]byte(tt.body)) }, time.Second, true)
			allowed, err := client.Check(context.Background(), "user:alice", "reader", "document:1")
			if allowed || !errors.Is(err, tt.want) {
				t.Fatalf("got (%v,%v), want (false,%v)", allowed, err, tt.want)
			}
			if strings.Contains(err.Error(), "private-token") {
				t.Error("error leaked credentials or upstream body")
			}
		})
	}
}

func TestCheckRequiresConfiguredModel(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unconfigured Check reached server") }, time.Second, false)
	allowed, err := client.Check(context.Background(), "user:alice", "reader", "document:1")
	if allowed || !errors.Is(err, ErrModelNotConfigured) {
		t.Fatalf("got (%v,%v)", allowed, err)
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.Write([]byte(`{"allowed":true}`))
	}, 20*time.Millisecond, true)
	allowed, err := client.Check(context.Background(), "user:alice", "reader", "document:1")
	if allowed || !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout got (%v,%v)", allowed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	allowed, err = client.Check(ctx, "user:alice", "reader", "document:1")
	if allowed || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation got (%v,%v)", allowed, err)
	}
}

func TestPingValidatesAuthenticationAndResponse(t *testing.T) {
	for _, body := range []string{`{"stores":[]}`, `null`, `{}`, `{"stores":null}`, `{"stores":false}`} {
		t.Run(body, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/stores" || r.URL.Query().Get("page_size") != "1" || r.Header.Get("Authorization") != "Bearer private-token" {
					t.Error("unexpected Ping request")
				}
				w.Write([]byte(body))
			}, time.Second, false)
			err := client.Ping(context.Background())
			if body == `{"stores":[]}` {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect was followed") }))
	defer target.Close()
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}, time.Second, false)
	if err := client.Ping(context.Background()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("got %v", err)
	}
}

func TestResponseBodyTimeout(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(80 * time.Millisecond)
		w.Write([]byte(`{"allowed":true}`))
	}, 20*time.Millisecond, true)
	allowed, err := client.Check(context.Background(), "user:alice", "reader", "document:1")
	if allowed || !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body timeout got (%v,%v)", allowed, err)
	}
}
