// Package integration runs the sensor against a real OpenLDAP server started
// with testcontainers. The tests are skipped with -short or when no container
// runtime is available.
package integration

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"emeland.io/modelsrv-ldap-sensor/internal/config"
	"emeland.io/modelsrv-ldap-sensor/internal/ldapclient"
	"emeland.io/modelsrv-ldap-sensor/internal/scan"
	"go.emeland.io/modelsrv/pkg/events"
)

const (
	adminDN       = "cn=admin,dc=example,dc=org"
	adminPassword = "admin"
	ldifInit      = "/tmp/people-groups.ldif"
)

// startOpenLDAP starts a server, loads the fixture and returns its ldap:// URL.
func startOpenLDAP(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test skipped with -short")
	}
	ctx := context.Background()

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "osixia/openldap:1.5.0",
			ExposedPorts: []string{"389/tcp"},
			Env: map[string]string{
				"LDAP_ORGANISATION":   "Example",
				"LDAP_DOMAIN":         "example.org",
				"LDAP_ADMIN_PASSWORD": adminPassword,
				"LDAP_TLS":            "false",
			},
			WaitingFor: wait.ForLog("slapd starting").WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		t.Skipf("cannot start OpenLDAP container (no container runtime?): %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })

	if err := ctr.CopyFileToContainer(ctx, "../testdata/people-groups.ldif", ldifInit, 0o644); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	code, out, err := ctr.Exec(ctx, []string{"ldapadd", "-x", "-D", adminDN, "-w", adminPassword, "-f", ldifInit})
	if err != nil || code != 0 {
		b := []byte{}
		if out != nil {
			b, _ = io.ReadAll(out)
		}
		t.Fatalf("ldapadd failed: code=%d err=%v out=%s", code, err, b)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := ctr.MappedPort(ctx, "389/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("ldap://%s:%s", host, port.Port())
}

func testConfig(url string) config.Config {
	cfg := config.Config{
		LDAP: config.LDAP{URL: url, BindDN: adminDN, BindPassword: adminPassword},
		Identities: config.Identities{Selector: config.Selector{
			BaseDN: "ou=people,dc=example,dc=org", IDAttribute: "entryUUID",
		}},
		Groups: config.Groups{Selector: config.Selector{
			BaseDN: "ou=groups,dc=example,dc=org", DescriptionAttribute: "description", IDAttribute: "entryUUID",
		}},
	}
	config.Normalize(&cfg)
	return cfg
}

type recorder struct{ evs []events.Event }

func (r *recorder) Emit(e events.Event) error { r.evs = append(r.evs, e); return nil }

func TestScanAgainstOpenLDAP(t *testing.T) {
	cfg := testConfig(startOpenLDAP(t))

	client, err := ldapclient.New(cfg.LDAP)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s := scan.New(cfg, client, rec, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := s.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}

	var ids, groups int
	for _, e := range rec.evs {
		if e.Operation != events.CreateOperation {
			t.Errorf("unexpected operation %v", e.Operation)
		}
		switch e.ResourceType {
		case events.IdentityResource:
			ids++
		case events.GroupResource:
			groups++
		}
	}
	if ids != 3 || groups != 2 {
		t.Fatalf("got %d identities and %d groups, want 3 and 2", ids, groups)
	}

	// A second scan of the unchanged directory is quiet.
	n := len(rec.evs)
	if err := s.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(rec.evs) != n {
		t.Fatalf("second scan emitted %d events", len(rec.evs)-n)
	}
}
