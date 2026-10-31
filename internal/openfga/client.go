// Package openfga provides the connection boundary for future authorization
// checks. It does not install a model, write grants, or protect application routes.
package openfga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotConfigured      = errors.New("OpenFGA connection is not configured")
	ErrModelNotConfigured = errors.New("OpenFGA store and authorization model are not configured")
	ErrUnauthorized       = errors.New("OpenFGA API authentication failed")
	ErrInvalidRequest     = errors.New("OpenFGA rejected the request")
	ErrUnavailable        = errors.New("OpenFGA is unavailable")
	ErrInvalidResponse    = errors.New("OpenFGA returned an invalid response")
	identifier            = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
)

type Config struct {
	APIURL               string
	APIToken             string
	StoreID              string
	AuthorizationModelID string
	Timeout              time.Duration
}

// ConfigFromEnv permits an absent connection for local runs. A configured
// connection can be deployed before a store/model exists; Check fails closed
// until both identifiers are supplied.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		APIURL:               getenv("OPENFGA_API_URL"),
		APIToken:             getenv("OPENFGA_API_TOKEN"),
		StoreID:              getenv("OPENFGA_STORE_ID"),
		AuthorizationModelID: getenv("OPENFGA_AUTHORIZATION_MODEL_ID"),
		Timeout:              2 * time.Second,
	}
	if raw := getenv("OPENFGA_TIMEOUT"); raw != "" {
		var err error
		cfg.Timeout, err = time.ParseDuration(raw)
		if err != nil {
			return Config{}, errors.New("OPENFGA_TIMEOUT must be a duration between 1ms and 30s")
		}
	}
	return cfg, cfg.validate()
}

func (cfg Config) validate() error {
	if cfg.Timeout < time.Millisecond || cfg.Timeout > 30*time.Second {
		return errors.New("OPENFGA_TIMEOUT must be a duration between 1ms and 30s")
	}
	if cfg.APIURL == "" {
		if cfg.APIToken != "" || cfg.StoreID != "" || cfg.AuthorizationModelID != "" {
			return errors.New("OPENFGA_API_URL is required when OpenFGA credentials or identifiers are configured")
		}
		return nil
	}
	u, err := url.Parse(cfg.APIURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("OPENFGA_API_URL must be an HTTP(S) origin without credentials, path, query, or fragment")
	}
	if strings.ContainsAny(cfg.APIToken, "\r\n") {
		return errors.New("OPENFGA_API_TOKEN must not contain line breaks")
	}
	if (cfg.StoreID == "") != (cfg.AuthorizationModelID == "") {
		return errors.New("OPENFGA_STORE_ID and OPENFGA_AUTHORIZATION_MODEL_ID must be configured together")
	}
	if cfg.StoreID != "" && (!identifier.MatchString(cfg.StoreID) || !identifier.MatchString(cfg.AuthorizationModelID)) {
		return errors.New("OPENFGA_STORE_ID and OPENFGA_AUTHORIZATION_MODEL_ID must be ULIDs")
	}
	return nil
}

type Client struct {
	config Config
	http   *http.Client
}

// New constructs one reusable HTTP client; it performs no network requests.
func New(cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.APIURL == "" {
		return nil, ErrNotConfigured
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &Client{config: cfg, http: &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
		// Never forward the bearer credential to a redirect target.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

// Ping verifies API authentication and datastore connectivity without requiring
// a store or changing persisted state. Health probes alone do not verify auth.
func (c *Client) Ping(ctx context.Context) error {
	var response struct {
		Stores *[]json.RawMessage `json:"stores"`
	}
	if err := c.request(ctx, http.MethodGet, "/stores?page_size=1", nil, &response); err != nil {
		return err
	}
	if response.Stores == nil {
		return ErrInvalidResponse
	}
	return nil
}

// Check keeps a denial (false, nil) distinct from an operational/configuration
// error (false, err). Callers must handle errors without granting access.
func (c *Client) Check(ctx context.Context, user, relation, object string) (bool, error) {
	if c.config.StoreID == "" || c.config.AuthorizationModelID == "" {
		return false, ErrModelNotConfigured
	}
	if user == "" || relation == "" || object == "" {
		return false, ErrInvalidRequest
	}
	input := struct {
		TupleKey struct {
			User     string `json:"user"`
			Relation string `json:"relation"`
			Object   string `json:"object"`
		} `json:"tuple_key"`
		ModelID string `json:"authorization_model_id"`
	}{ModelID: c.config.AuthorizationModelID}
	input.TupleKey.User, input.TupleKey.Relation, input.TupleKey.Object = user, relation, object
	var response struct {
		Allowed *bool `json:"allowed"`
	}
	if err := c.request(ctx, http.MethodPost, "/stores/"+c.config.StoreID+"/check", input, &response); err != nil {
		return false, err
	}
	if response.Allowed == nil {
		return false, ErrInvalidResponse
	}
	return *response.Allowed, nil
}

func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return ErrInvalidRequest
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.config.APIURL, "/")+path, body)
	if err != nil {
		return ErrInvalidRequest
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.config.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		category := ErrInvalidResponse
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			category = ErrUnauthorized
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			category = ErrUnavailable
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			category = ErrInvalidRequest
		}
		return fmt.Errorf("%w (HTTP %d)", category, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return transportError(err)
	}
	if len(data) > 64*1024 || json.Unmarshal(data, output) != nil {
		return ErrInvalidResponse
	}
	return nil
}

func transportError(err error) error {
	// Never return transport URLs, response bodies, or credential-bearing data.
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrUnavailable, context.DeadlineExceeded)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %w", ErrUnavailable, context.Canceled)
	}
	return ErrUnavailable
}

// CheckContext accepts only facts constructed by the application from its locked
// database snapshot. Stored grants must never coexist with this contextual model.
func (c *Client) CheckContext(ctx context.Context, user, relation, object string, facts []scheduling.Tuple) (bool, error) {
	if c == nil || c.config.StoreID == "" || c.config.AuthorizationModelID == "" {
		return false, ErrModelNotConfigured
	}
	if len(facts) > 100 {
		return false, ErrInvalidRequest
	}
	input := map[string]any{"tuple_key": map[string]string{"user": user, "relation": relation, "object": object}, "authorization_model_id": c.config.AuthorizationModelID, "consistency": "HIGHER_CONSISTENCY", "contextual_tuples": map[string]any{"tuple_keys": facts}}
	var response struct {
		Allowed *bool `json:"allowed"`
	}
	if err := c.request(ctx, http.MethodPost, "/stores/"+c.config.StoreID+"/check", input, &response); err != nil {
		return false, err
	}
	if response.Allowed == nil {
		return false, ErrInvalidResponse
	}
	return *response.Allowed, nil
}
