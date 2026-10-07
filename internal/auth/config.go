package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

type Config struct {
	Mode       string
	Issuer     string
	BaseURL    string
	ClientID   string
	ProjectID  string
	SessionKey []byte
}

func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		Mode:      getenv("AUTH_MODE"),
		Issuer:    strings.TrimSpace(getenv("ZITADEL_ISSUER")),
		BaseURL:   strings.TrimSpace(getenv("APP_BASE_URL")),
		ClientID:  strings.TrimSpace(getenv("ZITADEL_CLIENT_ID")),
		ProjectID: strings.TrimSpace(getenv("ZITADEL_PROJECT_ID")),
	}
	if c.Mode == "" {
		c.Mode = "oidc"
	}
	if c.Mode == "development" {
		return c, nil
	}
	key, err := base64.StdEncoding.DecodeString(getenv("AUTH_SESSION_KEY"))
	if err != nil || len(key) != 32 {
		return Config{}, errors.New("AUTH_SESSION_KEY must be a base64-encoded 32-byte key")
	}
	c.SessionKey = key
	return c, c.validate()
}

func (c Config) validate() error {
	if c.Mode == "development" {
		return nil
	}
	if c.Mode != "oidc" {
		return errors.New("AUTH_MODE must be oidc or development")
	}
	if c.ClientID == "" || c.ProjectID == "" {
		return errors.New("ZITADEL_CLIENT_ID and ZITADEL_PROJECT_ID are required")
	}
	if len(c.SessionKey) != 32 {
		return errors.New("AUTH_SESSION_KEY must decode to 32 bytes")
	}
	for name, value := range map[string]string{"ZITADEL_ISSUER": c.Issuer, "APP_BASE_URL": c.BaseURL} {
		u, err := url.Parse(value)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("%s must be an HTTP(S) origin without a path, query or credentials", name)
		}
		loopback := u.Hostname() == "localhost"
		if ip := net.ParseIP(u.Hostname()); ip != nil {
			loopback = ip.IsLoopback()
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
			return fmt.Errorf("%s must use HTTPS (HTTP is allowed only on loopback for local testing)", name)
		}
	}
	return nil
}
