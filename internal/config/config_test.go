package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emeland.io/modelsrv-ldap-sensor/internal/config"
)

func load(t *testing.T, body string) (config.Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return config.Load(p)
}

const minimal = `
ldap:
  url: ldap://localhost:389
identities:
  baseDN: ou=people,dc=x
groups:
  baseDN: ou=groups,dc=x
`

func TestDefaults(t *testing.T) {
	cfg, err := load(t, minimal)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != config.DefaultPollInterval || cfg.LDAP.PageSize != config.DefaultPageSize {
		t.Errorf("defaults not applied: %v %v", cfg.PollInterval, cfg.LDAP.PageSize)
	}
	if cfg.Identities.Filter != "(objectClass=inetOrgPerson)" || cfg.Groups.MemberAttribute != "member" ||
		cfg.Groups.NameAttribute != "cn" || cfg.Groups.Scope != "sub" {
		t.Errorf("selector defaults not applied: %+v", cfg)
	}
}

func TestPasswordFromEnv(t *testing.T) {
	t.Setenv("LDAP_BIND_PASSWORD", "secret")
	cfg, err := load(t, minimal+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LDAP.BindPassword != "secret" {
		t.Error("env password not used")
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"no url":         {"identities: {baseDN: a}\ngroups: {baseDN: b}", "ldap.url"},
		"bad scheme":     {strings.Replace(minimal, "ldap://", "http://", 1), "ldap or ldaps"},
		"starttls+ldaps": {strings.Replace(minimal, "ldap://localhost:389", "ldaps://h:636\n  startTLS: true", 1), "startTLS"},
		"bind w/o pw":    {minimal + "\n", ""},
		"no identity dn": {"ldap: {url: 'ldap://h'}\ngroups: {baseDN: b}", "identities.baseDN"},
		"bad scope":      {strings.Replace(minimal, "ou=groups,dc=x", "ou=groups,dc=x\n  scope: deep", 1), "scope"},
		"bad subscriber": {minimal + "subscribers: ['ftp://x']", "subscribers[0]"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			body := c.body
			if name == "bind w/o pw" {
				body = strings.Replace(minimal, "url: ldap://localhost:389", "url: ldap://localhost:389\n  bindDN: cn=admin", 1)
				c.want = "no password"
			}
			_, err := load(t, body)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}
