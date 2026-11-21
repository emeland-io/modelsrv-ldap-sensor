// Package ldapclient implements [dir.Searcher] on top of go-ldap.
package ldapclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"

	"github.com/go-ldap/ldap/v3"

	"emeland.io/modelsrv-ldap-sensor/internal/config"
	"emeland.io/modelsrv-ldap-sensor/internal/dir"
)

// Client searches an LDAP server. It opens a fresh connection per search, which
// keeps it robust against idle timeouts for the low poll rates used here.
type Client struct {
	cfg config.LDAP
	tls *tls.Config
}

var _ dir.Searcher = (*Client)(nil)

// New builds a client from the connection settings.
func New(cfg config.LDAP) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("ldap url: %w", err)
	}
	tc := &tls.Config{
		ServerName:         u.Hostname(),
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // explicit opt-in for testing
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ldap.caFile: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ldap.caFile %q contains no certificates", cfg.CAFile)
		}
		tc.RootCAs = pool
	}
	return &Client{cfg: cfg, tls: tc}, nil
}

func (c *Client) connect() (*ldap.Conn, error) {
	var opts []ldap.DialOpt
	opts = append(opts, ldap.DialWithTLSConfig(c.tls))
	conn, err := ldap.DialURL(c.cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", c.cfg.URL, err)
	}
	if c.cfg.StartTLS {
		if err := conn.StartTLS(c.tls); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}
	if c.cfg.BindDN != "" {
		if err := conn.Bind(c.cfg.BindDN, c.cfg.BindPassword); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("bind as %q: %w", c.cfg.BindDN, err)
		}
	} else if err := conn.UnauthenticatedBind(""); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("anonymous bind: %w", err)
	}
	return conn, nil
}

func scopeOf(s string) (int, error) {
	switch s {
	case "", "sub":
		return ldap.ScopeWholeSubtree, nil
	case "one":
		return ldap.ScopeSingleLevel, nil
	case "base":
		return ldap.ScopeBaseObject, nil
	}
	return 0, fmt.Errorf("unknown scope %q", s)
}

// Search runs q with the paged results control and returns all entries.
func (c *Client) Search(ctx context.Context, q dir.Query) ([]dir.Entry, error) {
	scope, err := scopeOf(q.Scope)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	req := ldap.NewSearchRequest(q.BaseDN, scope, ldap.NeverDerefAliases, 0, 0, false,
		q.Filter, q.Attributes, nil)
	res, err := conn.SearchWithPaging(req, c.cfg.PageSize)
	if err != nil {
		return nil, fmt.Errorf("search %q (%s): %w", q.BaseDN, q.Filter, err)
	}

	out := make([]dir.Entry, 0, len(res.Entries))
	for _, e := range res.Entries {
		attrs := make(map[string][]string, len(e.Attributes))
		for _, a := range e.Attributes {
			attrs[a.Name] = append([]string(nil), a.Values...)
		}
		out = append(out, dir.Entry{DN: e.DN, Attrs: attrs})
	}
	return out, nil
}
