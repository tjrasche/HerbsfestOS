package scheduling

import (
	"net"
	"net/url"
	"strings"
)

// absoluteLink uses only the configured application origin. Request Host and
// forwarded headers are untrusted and never participate in bearer URLs.
func (h *Handler) absoluteLink(path string) (string, error) {
	u, err := url.Parse(h.baseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", ErrUnavailable
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", ErrUnavailable
	}
	p, err := url.Parse(path)
	if err != nil || p.IsAbs() || p.Host != "" || p.User != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n") {
		return "", ErrInvalid
	}
	return strings.TrimRight(u.String(), "/") + p.String(), nil
}
