// Package config loads and validates the YAML configuration of the LDAP sensor.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultPollInterval is used when the config does not specify one.
const DefaultPollInterval = 5 * time.Minute

// DefaultPageSize is the LDAP paged-results size used when none is configured.
const DefaultPageSize = 500

// Config is the on-disk YAML configuration for the LDAP sensor.
type Config struct {
	// Subscribers are downstream model servers that receive the Identity and
	// Group events this sensor emits (e.g. "http://modelsrv:24000/api/").
	Subscribers []string `yaml:"subscribers"`

	// PollInterval is how often the directory is re-scanned.
	PollInterval time.Duration `yaml:"pollInterval"`

	LDAP       LDAP       `yaml:"ldap"`
	Identities Identities `yaml:"identities"`
	Groups     Groups     `yaml:"groups"`
}

// LDAP describes how to connect to the directory.
type LDAP struct {
	// URL is the ldap:// or ldaps:// URL of the server.
	URL string `yaml:"url"`
	// BindDN and BindPassword authenticate the search. Leave BindDN empty for
	// an anonymous bind. The password can also be supplied through the
	// environment variable LDAP_BIND_PASSWORD, which takes precedence.
	BindDN       string `yaml:"bindDN"`
	BindPassword string `yaml:"bindPassword"`
	// StartTLS upgrades a plain ldap:// connection with StartTLS.
	StartTLS bool `yaml:"startTLS"`
	// CAFile is an optional PEM file with additional trusted CA certificates.
	CAFile string `yaml:"caFile"`
	// InsecureSkipVerify disables certificate verification. Only for testing.
	InsecureSkipVerify bool `yaml:"insecureSkipVerify"`
	// PageSize is the page size of the paged results search control.
	PageSize uint32 `yaml:"pageSize"`
}

// Selector selects a set of directory entries and says how to read them.
type Selector struct {
	// BaseDN is the sub-tree to search.
	BaseDN string `yaml:"baseDN"`
	// Filter is the LDAP search filter selecting the entries to import.
	Filter string `yaml:"filter"`
	// Scope is one of "sub" (default), "one" or "base".
	Scope string `yaml:"scope"`
	// NameAttribute is the attribute used as the display name.
	NameAttribute string `yaml:"nameAttribute"`
	// DescriptionAttribute is an optional attribute used as the description.
	DescriptionAttribute string `yaml:"descriptionAttribute"`
	// IDAttribute is an optional attribute holding a stable identifier
	// (e.g. entryUUID on OpenLDAP or objectGUID on Active Directory). When
	// empty, identifiers are derived from the entry's DN.
	IDAttribute string `yaml:"idAttribute"`
}

// Identities selects the entries that become Identity resources.
type Identities struct {
	Selector `yaml:",inline"`
}

// Groups selects the entries that become Group resources.
type Groups struct {
	Selector `yaml:",inline"`
	// MemberAttribute lists the DNs of the members of a group.
	MemberAttribute string `yaml:"memberAttribute"`
}

// Load reads, normalizes and validates the YAML config at path.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	Normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Normalize trims values and fills in defaults.
func Normalize(cfg *Config) {
	cfg.LDAP.URL = strings.TrimSpace(cfg.LDAP.URL)
	if v := os.Getenv("LDAP_BIND_PASSWORD"); v != "" {
		cfg.LDAP.BindPassword = v
	}
	if cfg.LDAP.PageSize == 0 {
		cfg.LDAP.PageSize = DefaultPageSize
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}

	subs := make([]string, 0, len(cfg.Subscribers))
	for _, s := range cfg.Subscribers {
		if s = strings.TrimSpace(s); s != "" {
			subs = append(subs, s)
		}
	}
	cfg.Subscribers = subs

	normalizeSelector(&cfg.Identities.Selector, "(objectClass=inetOrgPerson)", "cn")
	normalizeSelector(&cfg.Groups.Selector, "(objectClass=groupOfNames)", "cn")
	if cfg.Groups.MemberAttribute == "" {
		cfg.Groups.MemberAttribute = "member"
	}
}

func normalizeSelector(s *Selector, defFilter, defName string) {
	s.BaseDN = strings.TrimSpace(s.BaseDN)
	if s.Filter == "" {
		s.Filter = defFilter
	}
	if s.Scope == "" {
		s.Scope = "sub"
	}
	if s.NameAttribute == "" {
		s.NameAttribute = defName
	}
}

// Validate checks that the configuration is usable.
func Validate(cfg Config) error {
	if cfg.LDAP.URL == "" {
		return fmt.Errorf("config: missing ldap.url")
	}
	u, err := url.Parse(cfg.LDAP.URL)
	if err != nil {
		return fmt.Errorf("config: ldap.url: invalid URL %q: %w", cfg.LDAP.URL, err)
	}
	if u.Scheme != "ldap" && u.Scheme != "ldaps" {
		return fmt.Errorf("config: ldap.url: %q must use ldap or ldaps scheme", cfg.LDAP.URL)
	}
	if u.Host == "" {
		return fmt.Errorf("config: ldap.url: %q must include a host", cfg.LDAP.URL)
	}
	if cfg.LDAP.StartTLS && u.Scheme == "ldaps" {
		return fmt.Errorf("config: ldap.startTLS cannot be combined with an ldaps URL")
	}
	if cfg.LDAP.BindDN != "" && cfg.LDAP.BindPassword == "" {
		return fmt.Errorf("config: ldap.bindDN is set but no password was given")
	}
	if err := validateSelector("identities", cfg.Identities.Selector); err != nil {
		return err
	}
	if err := validateSelector("groups", cfg.Groups.Selector); err != nil {
		return err
	}
	for i, s := range cfg.Subscribers {
		su, err := url.Parse(s)
		if err != nil || (su.Scheme != "http" && su.Scheme != "https") || su.Host == "" {
			return fmt.Errorf("config: subscribers[%d]: %q must be an http(s) URL with a host", i, s)
		}
	}
	return nil
}

func validateSelector(field string, s Selector) error {
	if s.BaseDN == "" {
		return fmt.Errorf("config: missing %s.baseDN", field)
	}
	switch s.Scope {
	case "sub", "one", "base":
	default:
		return fmt.Errorf("config: %s.scope %q must be sub, one or base", field, s.Scope)
	}
	return nil
}
