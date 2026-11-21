package mapping_test

import (
	"testing"

	"github.com/google/uuid"

	"emeland.io/modelsrv-ldap-sensor/internal/config"
	"emeland.io/modelsrv-ldap-sensor/internal/dir"
	"emeland.io/modelsrv-ldap-sensor/internal/mapping"
)

func testConfig() config.Config {
	cfg := config.Config{LDAP: config.LDAP{URL: "ldap://ldap.example.org:389"}}
	config.Normalize(&cfg)
	return cfg
}

func entry(dn string, kv ...string) dir.Entry {
	attrs := map[string][]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		attrs[kv[i]] = append(attrs[kv[i]], kv[i+1])
	}
	return dir.Entry{DN: dn, Attrs: attrs}
}

func TestNormalizeDN(t *testing.T) {
	a := mapping.NormalizeDN("CN=Alice, OU=People ,DC=Example,DC=org")
	b := mapping.NormalizeDN("cn=alice,ou=people,dc=example,dc=org")
	if a != b {
		t.Fatalf("expected equal, got %q and %q", a, b)
	}
}

func TestMapIdentitiesAndGroups(t *testing.T) {
	cfg := testConfig()
	ids := []dir.Entry{
		entry("uid=alice,ou=people,dc=example,dc=org", "cn", "Alice"),
		entry("uid=bob,ou=people,dc=example,dc=org", "cn", "Bob"),
	}
	groups := []dir.Entry{
		entry("cn=admins,ou=groups,dc=example,dc=org", "cn", "admins",
			"member", "UID=alice, ou=people,dc=example,dc=org",
			"member", "uid=alice,ou=people,dc=example,dc=org",
			"member", "uid=ghost,ou=people,dc=example,dc=org"),
	}

	res := mapping.Map(cfg, ids, groups)

	if len(res.Identities) != 2 || len(res.Groups) != 1 {
		t.Fatalf("got %d identities, %d groups", len(res.Identities), len(res.Groups))
	}
	aliceID := mapping.EntryID(cfg.LDAP.URL, "identity", cfg.Identities.Selector, ids[0])
	alice := res.Identities[aliceID]
	if alice == nil || alice.GetDisplayName() != "Alice" {
		t.Fatalf("alice not mapped: %v", alice)
	}
	if got := alice.GetAnnotations().GetValue(mapping.AnnotationIdentityDN); got != ids[0].DN {
		t.Errorf("identity DN annotation = %q", got)
	}
	if got := alice.GetAnnotations().GetValue(mapping.AnnotationIdentityURL); got != cfg.LDAP.URL {
		t.Errorf("identity URL annotation = %q", got)
	}

	for _, g := range res.Groups {
		members := g.GetMembers()
		if len(members) != 1 || members[0].EffectiveIdentityID() != aliceID {
			t.Errorf("expected only alice as member (deduplicated), got %d", len(members))
		}
	}
	if len(res.Skipped) != 1 {
		t.Errorf("expected the unknown member to be skipped, got %v", res.Skipped)
	}
}

func TestEntryIDIsStableAndScoped(t *testing.T) {
	sel := config.Selector{}
	e := entry("uid=alice,dc=x")
	a := mapping.EntryID("ldap://h1:389", "identity", sel, e)
	if a != mapping.EntryID("ldap://H1:389", "identity", sel, e) {
		t.Error("host case must not matter")
	}
	if a == mapping.EntryID("ldap://h2:389", "identity", sel, e) {
		t.Error("different servers must give different ids")
	}
	if a == mapping.EntryID("ldap://h1:389", "group", sel, e) {
		t.Error("identity and group must not collide")
	}
}

func TestEntryIDUsesIDAttribute(t *testing.T) {
	want := uuid.New()
	sel := config.Selector{IDAttribute: "entryUUID"}
	e := entry("uid=alice,dc=x", "entryUUID", want.String())
	if got := mapping.EntryID("ldap://h:389", "identity", sel, e); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// Survives a rename (DN change) and non-UUID values are hashed.
	moved := entry("uid=alice,ou=new,dc=x", "entryUUID", want.String())
	if got := mapping.EntryID("ldap://h:389", "identity", sel, moved); got != want {
		t.Errorf("id changed after move: %s", got)
	}
	odd := entry("uid=b,dc=x", "entryUUID", "not-a-uuid")
	if mapping.EntryID("ldap://h:389", "identity", sel, odd) == uuid.Nil {
		t.Error("expected a derived id")
	}
}

func TestDisplayNameFallsBackToRDN(t *testing.T) {
	cfg := testConfig()
	res := mapping.Map(cfg, []dir.Entry{entry("uid=carol,ou=people,dc=x")}, nil)
	for _, i := range res.Identities {
		if i.GetDisplayName() != "carol" {
			t.Errorf("display name = %q", i.GetDisplayName())
		}
	}
}
