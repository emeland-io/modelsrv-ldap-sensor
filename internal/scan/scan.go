// Package scan runs one directory scan and reconciles the result with the
// resources already emitted: new ones are created, changed ones updated and
// vanished ones deleted.
package scan

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"emeland.io/modelsrv-ldap-sensor/internal/config"
	"emeland.io/modelsrv-ldap-sensor/internal/dir"
	"emeland.io/modelsrv-ldap-sensor/internal/mapping"
	"go.emeland.io/modelsrv/pkg/events"
	"go.emeland.io/modelsrv/pkg/model/annotations"
	"go.emeland.io/modelsrv/pkg/model/iam"
)

// Emitter applies a domain event to the local model and forwards it.
type Emitter interface {
	Emit(events.Event) error
}

// Scanner scans a directory and emits Identity and Group events.
type Scanner struct {
	cfg      config.Config
	searcher dir.Searcher
	emitter  Emitter
	log      *zap.SugaredLogger

	seenIdentities map[uuid.UUID]string // id -> signature
	seenGroups     map[uuid.UUID]string
}

// New builds a scanner.
func New(cfg config.Config, s dir.Searcher, e Emitter, log *zap.SugaredLogger) *Scanner {
	if log == nil {
		log = zap.NewNop().Sugar()
	}
	return &Scanner{
		cfg: cfg, searcher: s, emitter: e, log: log,
		seenIdentities: map[uuid.UUID]string{},
		seenGroups:     map[uuid.UUID]string{},
	}
}

func attrs(sel config.Selector, extra ...string) []string {
	out := []string{sel.NameAttribute}
	for _, a := range append([]string{sel.DescriptionAttribute, sel.IDAttribute}, extra...) {
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

// ScanOnce reads the directory and emits the differences to the last scan.
// If any search fails nothing is emitted or deleted.
func (s *Scanner) ScanOnce(ctx context.Context) error {
	idEntries, err := s.searcher.Search(ctx, dir.Query{
		BaseDN: s.cfg.Identities.BaseDN, Scope: s.cfg.Identities.Scope,
		Filter: s.cfg.Identities.Filter, Attributes: attrs(s.cfg.Identities.Selector),
	})
	if err != nil {
		return fmt.Errorf("scan identities: %w", err)
	}
	grpEntries, err := s.searcher.Search(ctx, dir.Query{
		BaseDN: s.cfg.Groups.BaseDN, Scope: s.cfg.Groups.Scope,
		Filter: s.cfg.Groups.Filter, Attributes: attrs(s.cfg.Groups.Selector, s.cfg.Groups.MemberAttribute),
	})
	if err != nil {
		return fmt.Errorf("scan groups: %w", err)
	}

	res := mapping.Map(s.cfg, idEntries, grpEntries)
	for _, note := range res.Skipped {
		s.log.Debugw("skipped", "note", note)
	}

	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Identities first, so group members always resolve downstream.
	for id, ident := range res.Identities {
		sig := identitySig(ident)
		keep(s.upsert(events.IdentityResource, id, ident, sig, s.seenIdentities))
	}
	for id, g := range res.Groups {
		sig := groupSig(g)
		keep(s.upsert(events.GroupResource, id, g, sig, s.seenGroups))
	}
	// Groups are deleted before the identities they reference.
	for id := range s.seenGroups {
		if _, ok := res.Groups[id]; !ok {
			keep(s.remove(events.GroupResource, id, s.seenGroups))
		}
	}
	for id := range s.seenIdentities {
		if _, ok := res.Identities[id]; !ok {
			keep(s.remove(events.IdentityResource, id, s.seenIdentities))
		}
	}

	s.log.Infow("scan finished", "identities", len(res.Identities), "groups", len(res.Groups), "skipped", len(res.Skipped))
	return firstErr
}

func (s *Scanner) upsert(rt events.ResourceType, id uuid.UUID, obj any, sig string, seen map[uuid.UUID]string) error {
	prev, ok := seen[id]
	if ok && prev == sig {
		return nil
	}
	op := events.CreateOperation
	if ok {
		op = events.UpdateOperation
	}
	if err := s.emitter.Emit(events.Event{ResourceType: rt, Operation: op, ResourceId: id, Objects: []any{obj}}); err != nil {
		return fmt.Errorf("emit %s %s: %w", rt.String(), id, err)
	}
	seen[id] = sig
	return nil
}

func (s *Scanner) remove(rt events.ResourceType, id uuid.UUID, seen map[uuid.UUID]string) error {
	if err := s.emitter.Emit(events.Event{ResourceType: rt, Operation: events.DeleteOperation, ResourceId: id}); err != nil {
		return fmt.Errorf("delete %s %s: %w", rt.String(), id, err)
	}
	delete(seen, id)
	return nil
}

func identitySig(i iam.Identity) string {
	return strings.Join([]string{i.GetDisplayName(), i.GetDescription(), annSig(i.GetAnnotations())}, "\x00")
}

func groupSig(g iam.Group) string {
	var ids []string
	for _, m := range g.GetMembers() {
		ids = append(ids, m.EffectiveIdentityID().String())
	}
	sort.Strings(ids)
	return strings.Join([]string{g.GetDisplayName(), g.GetDescription(), annSig(g.GetAnnotations()), strings.Join(ids, ",")}, "\x00")
}

func annSig(a annotations.Annotations) string {
	var kv []string
	for k := range a.GetKeys() {
		kv = append(kv, k+"="+a.GetValue(k))
	}
	sort.Strings(kv)
	return strings.Join(kv, ";")
}
