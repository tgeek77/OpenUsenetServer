package store

import (
	"context"
	"sort"
	"strings"
	"strconv"
	"sync"
	"time"

	wmat "github.com/openusenet/openusenet/internal/wildmat"
)

type memGroup struct {
	Group
	nums []int64
}

type memArt struct {
	StoredArticle
	groups map[string]int64
}

// Memory is an in-process store for tests.
type Memory struct {
	mu      sync.Mutex
	groups  map[string]*memGroup
	arts    map[string]*memArt // msgid
	byNum   map[string]map[int64]*memArt
	history map[string]time.Time
}

func NewMemory() *Memory {
	return &Memory{
		groups:  map[string]*memGroup{},
		arts:    map[string]*memArt{},
		byNum:   map[string]map[int64]*memArt{},
		history: map[string]time.Time{},
	}
}

func (m *Memory) Close() error { return nil }

func (m *Memory) EnsureGroup(_ context.Context, name, desc, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status == "" {
		status = "y"
	}
	if g, ok := m.groups[name]; ok {
		if desc != "" {
			g.Description = desc
		}
		g.Status = status
		return nil
	}
	m.groups[name] = &memGroup{Group: Group{
		Name: name, Description: desc, Status: status, CreatedAt: time.Now(),
	}}
	m.byNum[name] = map[int64]*memArt{}
	return nil
}

func (m *Memory) EnsureGroups(ctx context.Context, groups []Group) error {
	for _, g := range groups {
		if err := m.EnsureGroup(ctx, g.Name, g.Description, g.Status); err != nil {
			return err
		}
	}
	return nil
}

func (m *Memory) CountGroups(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.groups), nil
}

func (m *Memory) ListGroups(_ context.Context, wildmat string) ([]Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Group
	for _, g := range m.groups {
		if wildmat != "" && !wmat.Match(wildmat, g.Name) {
			continue
		}
		out = append(out, g.Group)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Memory) GetGroup(_ context.Context, name string) (*Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[name]
	if !ok {
		return nil, nil
	}
	cp := g.Group
	return &cp, nil
}

func (m *Memory) ArticleNumbers(_ context.Context, group string, lo, hi int64) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[group]
	if !ok {
		return nil, nil
	}
	var out []int64
	for _, n := range g.nums {
		if (lo == 0 || n >= lo) && (hi == 0 || n <= hi) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (m *Memory) GetByNumber(_ context.Context, group string, num int64) (*StoredArticle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byNum[group][num]
	if !ok {
		return nil, nil
	}
	cp := a.StoredArticle
	cp.Num = num
	return &cp, nil
}

func (m *Memory) GetByMsgID(_ context.Context, msgid string) (*StoredArticle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.arts[msgid]
	if !ok {
		return nil, nil
	}
	cp := a.StoredArticle
	cp.Num = 0
	return &cp, nil
}

func (m *Memory) Overview(_ context.Context, group string, lo, hi int64) ([]OverviewRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[group]
	if !ok {
		return nil, nil
	}
	var out []OverviewRow
	for _, n := range g.nums {
		if (lo == 0 || n >= lo) && (hi == 0 || n <= hi) {
			a := m.byNum[group][n]
			out = append(out, OverviewRow{
				Num: n, Subject: a.Subject, From: a.From, Date: a.Date,
				MsgID: a.MessageID, Refs: a.Refs, Bytes: a.Bytes, Lines: a.Lines, Xref: a.Xref,
			})
		}
	}
	return out, nil
}

func (m *Memory) Header(_ context.Context, group string, lo, hi int64, header string) ([]OverviewRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[group]
	if !ok {
		return nil, nil
	}
	var out []OverviewRow
	for _, n := range g.nums {
		if (lo == 0 || n >= lo) && (hi == 0 || n <= hi) {
			a := m.byNum[group][n]
			out = append(out, OverviewRow{
				Num: n, Header: headerValue(header, a),
			})
		}
	}
	return out, nil
}

func headerValue(header string, a *memArt) string {
	switch strings.ToLower(header) {
	case "subject":
		return a.Subject
	case "from":
		return a.From
	case "date":
		return a.Date
	case "message-id":
		return a.MessageID
	case "references":
		return a.Refs
	case ":bytes":
		return itoa(a.Bytes)
	case ":lines":
		return itoa(a.Lines)
	case "xref":
		return a.Xref
	default:
		return headerFromRaw(a.Headers, header)
	}
}

func headerFromRaw(raw, name string) string {
	for _, line := range strings.Split(raw, "\r\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(k, name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func (m *Memory) NewNews(_ context.Context, wildmat string, since time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, a := range m.arts {
		if a.StoredAt.Before(since) {
			continue
		}
		ok := false
		for g := range a.groups {
			if wildmat == "" || wmat.Match(wildmat, g) {
				ok = true
				break
			}
		}
		if ok && !seen[a.MessageID] {
			seen[a.MessageID] = true
			out = append(out, a.MessageID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *Memory) NewGroups(_ context.Context, since time.Time) ([]Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Group
	for _, g := range m.groups {
		if !g.CreatedAt.Before(since) {
			out = append(out, g.Group)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Memory) HasMessageID(_ context.Context, msgid string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.history[msgid]
	return ok, nil
}

func (m *Memory) Post(_ context.Context, headers, body, msgid, subject, from, date, refs, xref string, bytes, lines int, groups []string) (*PostResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.history[msgid]; ok {
		return nil, ErrDuplicate
	}
	var used []string
	for _, g := range groups {
		if _, ok := m.groups[g]; ok {
			used = append(used, g)
		}
	}
	if len(used) == 0 {
		return nil, ErrNoGroup
	}
	art := &memArt{
		StoredArticle: StoredArticle{
			MessageID: msgid, Headers: headers, Body: body,
			Bytes: bytes, Lines: lines, StoredAt: time.Now(),
			Subject: subject, From: from, Date: date, Refs: refs,
		},
		groups: map[string]int64{},
	}
	nums := map[string]int64{}
	var xrefParts []string
	for _, g := range used {
		mg := m.groups[g]
		mg.High++
		if mg.Low == 0 {
			mg.Low = mg.High
		}
		mg.Count++
		n := mg.High
		mg.nums = append(mg.nums, n)
		m.byNum[g][n] = art
		art.groups[g] = n
		nums[g] = n
		xrefParts = append(xrefParts, g+":"+strconv.FormatInt(n, 10))
	}
	prefix := strings.TrimSpace(xref)
	if prefix != "" {
		art.Xref = prefix + " " + strings.Join(xrefParts, " ")
	} else {
		art.Xref = strings.Join(xrefParts, " ")
	}
	m.arts[msgid] = art
	m.history[msgid] = time.Now()
	return &PostResult{MessageID: msgid, Xref: art.Xref, Numbers: nums}, nil
}

func (m *Memory) Next(_ context.Context, group string, cur int64) (*StoredArticle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[group]
	if !ok {
		return nil, nil
	}
	for _, n := range g.nums {
		if n > cur {
			a := m.byNum[group][n]
			cp := a.StoredArticle
			cp.Num = n
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *Memory) Prev(_ context.Context, group string, cur int64) (*StoredArticle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[group]
	if !ok {
		return nil, nil
	}
	var found *StoredArticle
	for _, n := range g.nums {
		if n < cur {
			a := m.byNum[group][n]
			cp := a.StoredArticle
			cp.Num = n
			found = &cp
		}
	}
	return found, nil
}

var (
	ErrDuplicate = errStore("duplicate message-id")
	ErrNoGroup   = errStore("no such newsgroup")
)

type errStore string

func (e errStore) Error() string { return string(e) }
