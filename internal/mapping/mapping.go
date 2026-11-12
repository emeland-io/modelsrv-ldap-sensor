// Package mapping turns directory entries into EmELand Identity and Group
// resources. It has no I/O and is fully unit-testable.
package mapping

import (
	"net/url"
	"strings"

	"github.com/google/uuid"

	"emeland.io/modelsrv-ldap-sensor/internal/config"
	"emeland.io/modelsrv-ldap-sensor/internal/dir"
	"go.emeland.io/modelsrv/pkg/model/iam"
)

// Well-known annotation keys written by this sensor.
const (
	AnnotationIdentityDN  = "emeland.io/p2-identity-ldap-distinguished-name"
	AnnotationGroupDN     = "emeland.io/p2-group-ldap-distinguished-name"
	AnnotationIdentityURL = "emeland.io/p2-identity-ldap-url"
	AnnotationGroupURL    = "emeland.io/p2-group-ldap-url"
)

// idNamespace anchors the deterministic UUIDv5 derived for directory entries.
var idNamespace = uuid.MustParse("5d0e7a52-3f0c-5b6a-9d57-0a9c2f6e4b11")

// Result is the desired state derived from one directory scan.
type Result struct {
	Identities map[uuid.UUID]iam.Identity
	Groups     map[uuid.UUID]iam.Group
	// Skipped lists human-readable notes about entries or members ignored.
	Skipped []string
}

// NormalizeDN makes DNs comparable: trimmed and lower-cased with whitespace
// around separators removed. This is deliberately simple; it is good enough
// for matching member values against entry DNs of the same server.
func NormalizeDN(dn string) string {
	parts := strings.Split(dn, ",")
	for i, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		for j := range kv {
			kv[j] = strings.TrimSpace(kv[j])
		}
		parts[i] = strings.Join(kv, "=")
	}
	return strings.ToLower(strings.Join(parts, ","))
}

// EntryID derives the stable identifier of an entry. A configured ID attribute
// that holds a UUID is used as is; any other value of it, or the DN, is hashed
// into a UUIDv5 scoped to the server host.
func EntryID(serverURL, kind string, sel config.Selector, e dir.Entry) uuid.UUID {
	host := serverURL
	if u, err := url.Parse(serverURL); err == nil && u.Host != "" {
		host = strings.ToLower(u.Host)
	}
	key := NormalizeDN(e.DN)
	if sel.IDAttribute != "" {
		if v := e.First(sel.IDAttribute); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				return id
			}
			key = "id:" + v
		}
	}
	return uuid.NewSHA1(idNamespace, []byte(host+"|"+kind+"|"+key))
}

// Map converts identity and group entries into model resources. Group members
// that do not match any identity entry are recorded in Result.Skipped.
func Map(cfg config.Config, identities, groups []dir.Entry) Result {
	res := Result{
		Identities: make(map[uuid.UUID]iam.Identity, len(identities)),
		Groups:     make(map[uuid.UUID]iam.Group, len(groups)),
	}
	byDN := make(map[string]uuid.UUID, len(identities))

	for _, e := range identities {
		id := EntryID(cfg.LDAP.URL, "identity", cfg.Identities.Selector, e)
		ident := iam.NewIdentity(id)
		ident.SetDisplayName(displayName(cfg.Identities.Selector, e))
		ident.SetDescription(e.First(cfg.Identities.DescriptionAttribute))
		ident.GetAnnotations().Add(AnnotationIdentityDN, e.DN)
		ident.GetAnnotations().Add(AnnotationIdentityURL, cfg.LDAP.URL)
		res.Identities[id] = ident
		byDN[NormalizeDN(e.DN)] = id
	}

	for _, e := range groups {
		id := EntryID(cfg.LDAP.URL, "group", cfg.Groups.Selector, e)
		g := iam.NewGroup(id)
		g.SetDisplayName(displayName(cfg.Groups.Selector, e))
		g.SetDescription(e.First(cfg.Groups.DescriptionAttribute))
		g.GetAnnotations().Add(AnnotationGroupDN, e.DN)
		g.GetAnnotations().Add(AnnotationGroupURL, cfg.LDAP.URL)

		var members []*iam.IdentityRef
		seen := map[uuid.UUID]bool{}
		for _, m := range e.Values(cfg.Groups.MemberAttribute) {
			mid, ok := byDN[NormalizeDN(m)]
			if !ok {
				res.Skipped = append(res.Skipped, "group "+e.DN+": member "+m+" is not a known identity")
				continue
			}
			if seen[mid] {
				continue
			}
			seen[mid] = true
			members = append(members, &iam.IdentityRef{IdentityId: mid})
		}
		g.SetMembers(members)
		res.Groups[id] = g
	}
	return res
}

func displayName(sel config.Selector, e dir.Entry) string {
	if n := e.First(sel.NameAttribute); n != "" {
		return n
	}
	// Fall back to the leftmost RDN value so every resource has a name.
	rdn := strings.SplitN(e.DN, ",", 2)[0]
	if i := strings.Index(rdn, "="); i >= 0 {
		return strings.TrimSpace(rdn[i+1:])
	}
	return e.DN
}
