// Package httpapi exposes versioned, company-scoped Books application services.
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
)

type Principal struct {
	ID          string              `json:"id"`
	TokenSHA256 string              `json:"token_sha256"`
	Companies   map[string][]string `json:"companies"`
}
type Config struct {
	Schema         string      `json:"schema"`
	Listen         string      `json:"listen"`
	TLSCertificate string      `json:"tls_certificate,omitempty"`
	TLSKey         string      `json:"tls_key,omitempty"`
	AllowedOrigins []string    `json:"allowed_origins,omitempty"`
	Principals     []Principal `json:"principals"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	f, e := os.Open(path)
	if e != nil {
		return c, apperr.Wrap(apperr.Unavailable, "SERVER_CONFIG_UNAVAILABLE", "server configuration could not be opened", e)
	}
	defer func() { _ = f.Close() }()
	info, e := f.Stat()
	if e != nil {
		return c, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return c, apperr.New(apperr.Invalid, "SERVER_CONFIG_PERMISSIONS", "server configuration must be a private regular file (mode 0600)")
	}
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, apperr.New(apperr.Input, "SERVER_CONFIG_INVALID", "server configuration is not valid JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, apperr.New(apperr.Input, "SERVER_CONFIG_INVALID", "server configuration must contain exactly one JSON value")
	}
	return c, c.Validate()
}

var principalPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func (c Config) Validate() error {
	bad := func(message string) error { return apperr.New(apperr.Invalid, "SERVER_CONFIG_INVALID", message) }
	if c.Schema != "books.server/v1" && c.Schema != "books.server/v2" {
		return bad("server schema must be books.server/v1 or books.server/v2")
	}
	host, port, e := net.SplitHostPort(c.Listen)
	if e != nil || port == "" {
		return bad("listen must be an explicit IP address and port")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return bad("listen must use an IP literal")
	}
	if (c.TLSCertificate == "") != (c.TLSKey == "") {
		return bad("TLS certificate and key must be configured together")
	}
	if !ip.IsLoopback() && c.TLSCertificate == "" {
		return bad("non-loopback listeners require TLS")
	}
	origins := map[string]bool{}
	for _, origin := range c.AllowedOrigins {
		u, e := url.Parse(origin)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || origins[origin] {
			return bad("allowed origins must be unique exact origins without paths")
		}
		if u.Scheme != "https" {
			p := net.ParseIP(u.Hostname())
			if u.Scheme != "http" || (u.Hostname() != "localhost" && (p == nil || !p.IsLoopback())) {
				return bad("origins must use HTTPS, except explicit local development origins")
			}
		}
		origins[origin] = true
	}
	if len(c.Principals) < 1 || len(c.Principals) > 100 {
		return bad("configure between 1 and 100 principals")
	}
	ids := map[string]bool{}
	tokens := map[string]bool{}
	for _, p := range c.Principals {
		if !principalPattern.MatchString(p.ID) || ids[p.ID] {
			return bad("principal IDs must be unique simple identifiers")
		}
		ids[p.ID] = true
		raw, e := hex.DecodeString(p.TokenSHA256)
		if e != nil || len(raw) != sha256.Size || p.TokenSHA256 != strings.ToLower(p.TokenSHA256) || tokens[p.TokenSHA256] {
			return bad("each principal requires a unique lowercase SHA-256 credential digest")
		}
		tokens[p.TokenSHA256] = true
		if len(p.Companies) == 0 {
			return bad("each principal requires company grants")
		}
		for key, grants := range p.Companies {
			if e := booksconfig.ValidateCompanyKey(key); e != nil {
				return bad("invalid company key in grants")
			}
			found := map[string]bool{}
			for _, grant := range grants {
				if found[grant] || (grant != "read" && grant != "import" && grant != "post" && (c.Schema != "books.server/v2" || grant != "manage")) {
					return bad("grants must be unique read, import, post, or (v2 only) manage values")
				}
				found[grant] = true
			}
			if !found["read"] || c.Schema == "books.server/v1" && found["post"] && !found["import"] {
				return bad("all grants require read; v1 post also requires import")
			}
		}
	}
	return nil
}
