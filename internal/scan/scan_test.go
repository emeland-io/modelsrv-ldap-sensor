package scan_test

import (
	"context"
	"errors"
	"testing"

	"emeland.io/modelsrv-ldap-sensor/internal/config"
	"emeland.io/modelsrv-ldap-sensor/internal/dir"
	"emeland.io/modelsrv-ldap-sensor/internal/scan"
	"go.emeland.io/modelsrv/pkg/events"
)

type fakeDir struct {
	identities, groups []dir.Entry
	err                error
}

func (f *fakeDir) Search(_ context.Context, q dir.Query) ([]dir.Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	if q.BaseDN == "ou=people,dc=x" {
		return f.identities, nil
	}
	return f.groups, nil
}

type recorder struct{ evs []events.Event }

func (r *recorder) Emit(e events.Event) error { r.evs = append(r.evs, e); return nil }

func (r *recorder) count(rt events.ResourceType, op events.Operation) int {
	n := 0
	for _, e := range r.evs {
		if e.ResourceType == rt && e.Operation == op {
			n++
		}
	}
	return n
}

func cfg() config.Config {
	c := config.Config{
		LDAP:       config.LDAP{URL: "ldap://h:389"},
		Identities: config.Identities{Selector: config.Selector{BaseDN: "ou=people,dc=x"}},
		Groups:     config.Groups{Selector: config.Selector{BaseDN: "ou=groups,dc=x"}},
	}
	config.Normalize(&c)
	return c
}

func person(uid, name string) dir.Entry {
	return dir.Entry{DN: "uid=" + uid + ",ou=people,dc=x", Attrs: map[string][]string{"cn": {name}}}
}

func group(name string, members ...string) dir.Entry {
	m := []string{}
	for _, u := range members {
		m = append(m, "uid="+u+",ou=people,dc=x")
	}
	return dir.Entry{DN: "cn=" + name + ",ou=groups,dc=x", Attrs: map[string][]string{"cn": {name}, "member": m}}
}

func TestCreateUpdateDelete(t *testing.T) {
	fd := &fakeDir{
		identities: []dir.Entry{person("alice", "Alice"), person("bob", "Bob")},
		groups:     []dir.Entry{group("admins", "alice")},
	}
	rec := &recorder{}
	s := scan.New(cfg(), fd, rec, nil)
	ctx := context.Background()

	if err := s.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.count(events.IdentityResource, events.CreateOperation) != 2 || rec.count(events.GroupResource, events.CreateOperation) != 1 {
		t.Fatalf("unexpected first-scan events: %v", rec.evs)
	}

	// An unchanged directory emits nothing.
	before := len(rec.evs)
	if err := s.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(rec.evs) != before {
		t.Fatalf("unchanged scan emitted %d events", len(rec.evs)-before)
	}

	// Rename a user and change group membership: two updates.
	fd.identities = []dir.Entry{person("alice", "Alice A."), person("bob", "Bob")}
	fd.groups = []dir.Entry{group("admins", "alice", "bob")}
	if err := s.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.count(events.IdentityResource, events.UpdateOperation) != 1 || rec.count(events.GroupResource, events.UpdateOperation) != 1 {
		t.Fatalf("expected one identity and one group update: %v", rec.evs)
	}

	// Remove bob and the group: group deleted before identity.
	fd.identities = []dir.Entry{person("alice", "Alice A.")}
	fd.groups = nil
	rec.evs = nil
	if err := s.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(rec.evs) != 2 || rec.evs[0].ResourceType != events.GroupResource ||
		rec.evs[1].ResourceType != events.IdentityResource ||
		rec.evs[0].Operation != events.DeleteOperation || rec.evs[1].Operation != events.DeleteOperation {
		t.Fatalf("unexpected deletes: %v", rec.evs)
	}
}

func TestFailedSearchChangesNothing(t *testing.T) {
	fd := &fakeDir{identities: []dir.Entry{person("alice", "Alice")}}
	rec := &recorder{}
	s := scan.New(cfg(), fd, rec, nil)
	if err := s.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec.evs = nil

	fd.err = errors.New("server down")
	if err := s.ScanOnce(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if len(rec.evs) != 0 {
		t.Fatalf("failed scan must not delete anything, got %v", rec.evs)
	}
}
