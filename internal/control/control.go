// Package control processes Usenet hierarchy control messages (RFC 5537):
// newgroup, rmgroup, checkgroups — and decides whether to apply them.
package control

import (
	"bufio"
	"context"
	"log"
	"strings"

	"openusenet/internal/article"
	"openusenet/internal/store"
)

// Builtin control newsgroups (always seeded).
var BuiltinGroups = []store.Group{
	{Name: "control", Description: "Control messages (general)", Status: "y", Origin: store.OriginLocal},
	{Name: "control.cancel", Description: "Cancel control messages", Status: "y", Origin: store.OriginLocal},
	{Name: "control.newgroup", Description: "Newgroup control messages", Status: "y", Origin: store.OriginLocal},
	{Name: "control.rmgroup", Description: "Rmgroup control messages", Status: "y", Origin: store.OriginLocal},
	{Name: "control.checkgroups", Description: "Checkgroups control messages", Status: "y", Origin: store.OriginLocal},
}

// Kind is a hierarchy control verb (not cancel).
type Kind string

const (
	KindNone        Kind = ""
	KindNewgroup    Kind = "newgroup"
	KindRmgroup     Kind = "rmgroup"
	KindCheckgroups Kind = "checkgroups"
)

// Message is a parsed hierarchy control request.
type Message struct {
	Kind        Kind
	Group       string // newgroup/rmgroup target
	Moderated   bool   // newgroup … moderated
	Description string
	Hierarchy   []string // checkgroups patterns from Control: args
	Entries     []store.Group // checkgroups body
}

// Parse extracts a hierarchy control message. Cancel is handled elsewhere.
func Parse(art *article.Article) (Message, bool) {
	if art == nil {
		return Message{}, false
	}
	ctrl := strings.TrimSpace(art.Get("Control"))
	if ctrl == "" {
		return Message{}, false
	}
	fields := strings.Fields(ctrl)
	if len(fields) == 0 {
		return Message{}, false
	}
	verb := strings.ToLower(fields[0])
	switch verb {
	case "cancel":
		return Message{}, false
	case "newgroup":
		if len(fields) < 2 || !article.ValidGroupName(fields[1]) {
			return Message{}, false
		}
		m := Message{Kind: KindNewgroup, Group: fields[1]}
		if len(fields) >= 3 && strings.EqualFold(fields[2], "moderated") {
			m.Moderated = true
		}
		m.Description = descriptionFromNewgroupBody(art.Body, m.Group)
		return m, true
	case "rmgroup":
		if len(fields) < 2 || !article.ValidGroupName(fields[1]) {
			return Message{}, false
		}
		return Message{Kind: KindRmgroup, Group: fields[1]}, true
	case "checkgroups":
		m := Message{Kind: KindCheckgroups}
		for _, a := range fields[1:] {
			if strings.HasPrefix(a, "#") {
				break
			}
			if a == "!" || strings.HasPrefix(a, "!") {
				continue // exclusion tokens; handled loosely via body list
			}
			m.Hierarchy = append(m.Hierarchy, a)
		}
		m.Entries = parseCheckgroupsBody(art.Body)
		return m, true
	default:
		return Message{}, false
	}
}

func descriptionFromNewgroupBody(body, group string) string {
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "For your newsgroups file:" {
			if sc.Scan() {
				return parseNewsgroupsLine(sc.Text(), group)
			}
			break
		}
	}
	return ""
}

func parseNewsgroupsLine(line, expectGroup string) string {
	line = strings.TrimRight(line, "\r")
	name, desc, ok := splitGroupDesc(line)
	if !ok {
		return ""
	}
	if expectGroup != "" && !strings.EqualFold(name, expectGroup) {
		// Still accept description if the line is for our group with whitespace issues.
		if !strings.HasPrefix(strings.ToLower(line), strings.ToLower(expectGroup)) {
			return ""
		}
	}
	return desc
}

func parseCheckgroupsBody(body string) []store.Group {
	var out []store.Group
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip MIME/pgp noise.
		if strings.HasPrefix(line, "--") || strings.HasPrefix(line, "Content-") {
			continue
		}
		name, desc, ok := splitGroupDesc(line)
		if !ok || !article.ValidGroupName(name) {
			continue
		}
		status := "y"
		if strings.HasSuffix(desc, " (Moderated)") {
			status = "m"
			desc = strings.TrimSuffix(desc, " (Moderated)")
		}
		out = append(out, store.Group{Name: name, Description: desc, Status: status, Origin: store.OriginControl})
	}
	return out
}

func splitGroupDesc(line string) (name, desc string, ok bool) {
	// Prefer tab; also allow multiple spaces as used in some checkgroups.
	if i := strings.IndexByte(line, '\t'); i >= 0 {
		name = strings.TrimSpace(line[:i])
		desc = strings.TrimSpace(line[i+1:])
		return name, desc, name != ""
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	name = fields[0]
	desc = strings.TrimSpace(line[len(name):])
	return name, desc, true
}

// Policy decides whether to apply a hierarchy control.
type Policy struct {
	// AcceptAll applies all well-formed hierarchy controls (hobbyist default).
	// When false, From must match an allow pattern (simple substring/suffix match).
	AcceptAll bool
	// AllowFrom are lowercase email/patterns; empty + !AcceptAll → drop.
	AllowFrom []string
}

// DefaultPolicy accepts hierarchy controls (PGP verify comes later).
func DefaultPolicy() Policy {
	return Policy{AcceptAll: true}
}

// Allowed reports whether this article's sender may apply the control.
func (p Policy) Allowed(art *article.Article, msg Message) bool {
	if msg.Kind == KindNone {
		return false
	}
	if p.AcceptAll {
		return true
	}
	from := strings.ToLower(strings.TrimSpace(art.Get("From")))
	if from == "" {
		return false
	}
	for _, a := range p.AllowFrom {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if a == "*" || strings.Contains(from, a) {
			return true
		}
	}
	return false
}

// Result summarizes what was applied.
type Result struct {
	Applied bool
	Kind    Kind
	Detail  string
	Created int
	Updated int
	Disabled int
}

// Apply mutates the active file according to msg. Caller still stores/feeds the article.
func Apply(ctx context.Context, st store.Store, msg Message, lg *log.Logger) (Result, error) {
	res := Result{Kind: msg.Kind}
	switch msg.Kind {
	case KindNewgroup:
		status := "y"
		if msg.Moderated {
			status = "m"
		}
		if err := st.ApplyControlGroup(ctx, msg.Group, msg.Description, status); err != nil {
			return res, err
		}
		res.Applied = true
		res.Created = 1
		res.Detail = msg.Group
		if lg != nil {
			lg.Printf("control newgroup %s status=%s", msg.Group, status)
		}
	case KindRmgroup:
		if err := st.DisableControlGroup(ctx, msg.Group); err != nil {
			return res, err
		}
		res.Applied = true
		res.Disabled = 1
		res.Detail = msg.Group
		if lg != nil {
			lg.Printf("control rmgroup %s", msg.Group)
		}
	case KindCheckgroups:
		nCreate, nUpdate, nDisable, err := applyCheckgroups(ctx, st, msg)
		if err != nil {
			return res, err
		}
		res.Applied = true
		res.Created, res.Updated, res.Disabled = nCreate, nUpdate, nDisable
		res.Detail = "checkgroups"
		if lg != nil {
			lg.Printf("control checkgroups entries=%d created=%d updated=%d disabled=%d",
				len(msg.Entries), nCreate, nUpdate, nDisable)
		}
	}
	return res, nil
}

func applyCheckgroups(ctx context.Context, st store.Store, msg Message) (created, updated, disabled int, err error) {
	want := make(map[string]store.Group, len(msg.Entries))
	for _, g := range msg.Entries {
		want[g.Name] = g
		existing, err := st.GetGroup(ctx, g.Name)
		if err != nil {
			return created, updated, disabled, err
		}
		if existing == nil {
			if err := st.ApplyControlGroup(ctx, g.Name, g.Description, g.Status); err != nil {
				return created, updated, disabled, err
			}
			created++
			continue
		}
		if err := st.ApplyControlGroup(ctx, g.Name, g.Description, g.Status); err != nil {
			return created, updated, disabled, err
		}
		updated++
	}
	// Soft-remove ISC/control groups in the affected hierarchy that are missing from the list.
	hier := msg.Hierarchy
	if len(hier) == 0 && len(msg.Entries) > 0 {
		hier = inferHierarchy(msg.Entries)
	}
	if len(hier) == 0 || len(want) == 0 {
		return created, updated, disabled, nil
	}
	all, err := st.ListGroups(ctx, "")
	if err != nil {
		return created, updated, disabled, err
	}
	for _, g := range all {
		if _, ok := want[g.Name]; ok {
			continue
		}
		if g.Origin != store.OriginISC && g.Origin != store.OriginControl && g.Origin != "" {
			continue // never auto-disable local/admin
		}
		if !inHierarchy(g.Name, hier) {
			continue
		}
		if g.Status == "n" {
			continue
		}
		if err := st.DisableControlGroup(ctx, g.Name); err != nil {
			return created, updated, disabled, err
		}
		disabled++
	}
	return created, updated, disabled, nil
}

func inferHierarchy(entries []store.Group) []string {
	roots := map[string]bool{}
	for _, g := range entries {
		if i := strings.IndexByte(g.Name, '.'); i > 0 {
			roots[g.Name[:i]+".*"] = true
		}
	}
	out := make([]string, 0, len(roots))
	for r := range roots {
		out = append(out, r)
	}
	return out
}

func inHierarchy(name string, patterns []string) bool {
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, ".*") {
			prefix := strings.TrimSuffix(p, ".*")
			if name == prefix || strings.HasPrefix(name, prefix+".") {
				return true
			}
			continue
		}
		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(name, prefix) {
				return true
			}
			continue
		}
		if name == p || strings.HasPrefix(name, p+".") {
			return true
		}
	}
	return false
}

// SeedBuiltinGroups ensures control.* exist (local origin; ISC cannot create them first-wins differently).
func SeedBuiltinGroups(ctx context.Context, st store.Store) error {
	for _, g := range BuiltinGroups {
		existing, err := st.GetGroup(ctx, g.Name)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if err := st.EnsureGroup(ctx, g.Name, g.Description, g.Status); err != nil {
			return err
		}
	}
	return nil
}
