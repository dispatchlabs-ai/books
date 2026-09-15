// Package httpapi exposes versioned, company-scoped Books application services.
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/dispatchlabs-ai/books/internal/operations"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dispatchlabs-ai/books/internal/apperr"
	booksconfig "github.com/dispatchlabs-ai/books/internal/config"
)

type Principal struct {
	Registry    []string            `json:"registry,omitempty"`
	ID          string              `json:"id"`
	TokenSHA256 string              `json:"token_sha256"`
	Companies   map[string][]string `json:"companies"`
	Databases   map[string][]string `json:"databases,omitempty"`
}
type DatabaseConfig struct {
	Path string `json:"path"`
	UUID string `json:"uuid"`
}
type Config struct {
	ArtifactDirectory string                    `json:"artifact_directory,omitempty"`
	Databases         map[string]DatabaseConfig `json:"databases,omitempty"`
	Schema            string                    `json:"schema"`
	Listen            string                    `json:"listen"`
	TLSCertificate    string                    `json:"tls_certificate,omitempty"`
	TLSKey            string                    `json:"tls_key,omitempty"`
	AllowedOrigins    []string                  `json:"allowed_origins,omitempty"`
	Principals        []Principal               `json:"principals"`
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
	if c.ArtifactDirectory != "" && !filepath.IsAbs(c.ArtifactDirectory) {
		return bad("artifact_directory must be absolute")
	}
	if c.Schema != "books.server/v1" && c.Schema != "books.server/v2" && c.Schema != "books.server/v3" && c.Schema != "books.server/v4" {
		return bad("server schema must be books.server/v1, v2, v3 or v4")
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
	if (c.Schema != "books.server/v3" && c.Schema != "books.server/v4") && len(c.Databases) > 0 {
		return bad("database handles require v3")
	}
	for key, db := range c.Databases {
		if !principalPattern.MatchString(key) || !filepath.IsAbs(db.Path) || (db.UUID != "" && !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(db.UUID)) || (db.UUID == "" && c.Schema != "books.server/v4") {
			return bad("database handles require a simple key, absolute path and UUID")
		}
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
		if !operations.ValidRegistryGrants(p.Registry) || (len(p.Registry) > 0 && c.Schema != "books.server/v4") {
			return bad("registry read/manage grants require v4")
		}
		if len(p.Companies) == 0 && len(p.Databases) == 0 && len(p.Registry) == 0 {
			return bad("each principal requires explicit company or database grants")
		}
		if (c.Schema != "books.server/v3" && c.Schema != "books.server/v4") && len(p.Databases) > 0 {
			return bad("database grants require v3")
		}
		for key, grants := range p.Databases {
			if _, ok := c.Databases[key]; !ok {
				return bad("database grant references unknown handle")
			}
			found := map[string]bool{}
			for _, grant := range grants {
				if found[grant] || (grant != "read" && grant != "manage" && (grant != "admin" || c.Schema != "books.server/v4")) {
					return bad("database grants must be unique read or manage values")
				}
				found[grant] = true
			}
			if c.Databases[key].UUID == "" && !found["admin"] {
				return bad("uninitialized database targets require admin authority")
			}
			if !found["read"] {
				return bad("database grants require read")
			}
		}
		for key, grants := range p.Companies {
			if e := booksconfig.ValidateCompanyKey(key); e != nil && (key != "*" || c.Schema != "books.server/v4") {
				return bad("invalid company key in grants")
			}
			found := map[string]bool{}
			for _, grant := range grants {
				if found[grant] || (grant != "read" && grant != "import" && grant != "post" && grant != "budget" && ((c.Schema != "books.server/v2" && c.Schema != "books.server/v3" && c.Schema != "books.server/v4") || grant != "manage")) {
					return bad("grants must be unique read, import, post, budget, or (v2+) manage values")
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
