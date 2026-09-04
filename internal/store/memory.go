package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"strconv"
	"sync"
	"time"

	wmat "openusenet/internal/wildmat"
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
	mu       sync.Mutex
	groups   map[string]*memGroup
	arts     map[string]*memArt // msgid
	byNum    map[string]map[int64]*memArt
	history  map[string]time.Time
	users    map[string]*User
	peers    map[int64]*Peer
	nextUID  int64
	nextPID  int64
	nextAID  int64
	nextFQID int64
	subs     map[int64]map[string]time.Time // userID -> group -> subscribed_at
	reads    map[int64]map[string]int64     // userID -> group -> last_read_num
	accepts  map[string][]memAccept
	alerts   []memAlert
	binQuota map[string]int
	feedQ    []FeedQueueItem
}

func NewMemory() *Memory {
	return &Memory{
		groups:  map[string]*memGroup{},
		arts:    map[string]*memArt{},
		byNum:   map[string]map[int64]*memArt{},
		history: map[string]time.Time{},
		users:   map[string]*User{},
		peers:   map[int64]*Peer{},
		subs:    map[int64]map[string]time.Time{},
		reads:   map[int64]map[string]int64{},
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

func (m *Memory) CountArticles(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.arts), nil
}

func (m *Memory) RecentArticles(_ context.Context, limit int) ([]StoredArticle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 {
		limit = 50
	}
	var out []StoredArticle
	for _, a := range m.arts {
		out = append(out, a.StoredArticle)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StoredAt.After(out[j].StoredAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) SearchGroups(_ context.Context, query string, busyOnly bool, limit int) ([]Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var out []Group
	for _, g := range m.groups {
		if busyOnly && g.Count == 0 {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(g.Name), query) {
			continue
		}
		out = append(out, g.Group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) SearchArticles(_ context.Context, query, group string, limit, offset int) ([]ArticleSearchHit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	group = strings.TrimSpace(group)
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return nil, nil
	}
	var hits []ArticleSearchHit
	for _, art := range m.arts {
		blob := strings.ToLower(art.Subject + " " + art.From + " " + art.Body)
		ok := true
		for _, tok := range tokens {
			tok = strings.Trim(tok, `"'`)
			if tok == "" {
				continue
			}
			if strings.HasPrefix(tok, "-") {
				if strings.Contains(blob, strings.TrimPrefix(tok, "-")) {
					ok = false
					break
				}
				continue
			}
			if !strings.Contains(blob, tok) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		var groups []string
		var primary string
		var num int64
		for g, n := range art.groups {
			groups = append(groups, g)
			if group != "" && g == group {
				primary, num = g, n
			}
		}
		sort.Strings(groups)
		if primary == "" && len(groups) > 0 {
			primary = groups[0]
			num = art.groups[primary]
		}
		if group != "" && primary != group {
			continue
		}
		snippet := art.Body
		if len(snippet) > 160 {
			snippet = snippet[:160] + "…"
		}
		hits = append(hits, ArticleSearchHit{
			MessageID: art.MessageID, Subject: art.Subject, From: art.From, Date: art.Date,
			StoredAt: art.StoredAt.UTC().Format(time.RFC3339), Xref: art.Xref,
			Groups: groups, Group: primary, Num: num, Rank: 1, Snippet: snippet,
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].StoredAt > hits[j].StoredAt
	})
	if offset >= len(hits) {
		return nil, nil
	}
	hits = hits[offset:]
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
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
		mg, ok := m.groups[g]
		if !ok || mg.Status == "n" {
			continue
		}
		used = append(used, g)
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

func (m *Memory) CountUsers(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *Memory) ListUsers(_ context.Context) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (m *Memory) GetUser(_ context.Context, username string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[strings.TrimSpace(username)]
	if !ok {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (m *Memory) CreateUser(_ context.Context, u User) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.Username = strings.TrimSpace(u.Username)
	if u.Username == "" || u.PasswordHash == "" {
		return nil, errors.New("username and password required")
	}
	if _, ok := m.users[u.Username]; ok {
		return nil, ErrUserExists
	}
	if u.Role == "" {
		u.Role = RoleUser
	}
	m.nextUID++
	u.ID = m.nextUID
	u.CreatedAt = time.Now().UTC()
	cp := u
	m.users[u.Username] = &cp
	return &u, nil
}

func (m *Memory) UpdateUser(_ context.Context, username string, role string, canPost, disabled *bool, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[strings.TrimSpace(username)]
	if !ok {
		return ErrUserNotFound
	}
	if role != "" {
		u.Role = role
	}
	if canPost != nil {
		u.CanPost = *canPost
	}
	if disabled != nil {
		u.Disabled = *disabled
	}
	if passwordHash != "" {
		u.PasswordHash = passwordHash
	}
	return nil
}

func (m *Memory) DeleteUser(_ context.Context, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[strings.TrimSpace(username)]; !ok {
		return ErrUserNotFound
	}
	delete(m.users, strings.TrimSpace(username))
	return nil
}

func (m *Memory) CountPeers(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.peers), nil
}

func (m *Memory) ListPeers(_ context.Context) ([]Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Peer, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host == out[j].Host {
			return out[i].Port < out[j].Port
		}
		return out[i].Host < out[j].Host
	})
	return out, nil
}

func (m *Memory) ListEnabledPeers(ctx context.Context) ([]Peer, error) {
	all, err := m.ListPeers(ctx)
	if err != nil {
		return nil, err
	}
	var out []Peer
	for _, p := range all {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *Memory) GetPeer(_ context.Context, id int64) (*Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *Memory) CreatePeer(_ context.Context, peer Peer) (*Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	peer = peer.Normalize()
	if peer.Host == "" {
		return nil, errors.New("host required")
	}
	m.nextPID++
	peer.ID = m.nextPID
	peer.Created = time.Now().UTC()
	cp := peer
	m.peers[peer.ID] = &cp
	return &peer, nil
}

func (m *Memory) UpdatePeer(_ context.Context, peer Peer) (*Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[peer.ID]
	if !ok {
		return nil, ErrPeerNotFound
	}
	peer = peer.Normalize()
	if peer.Host == "" {
		return nil, errors.New("host required")
	}
	*p = peer
	cp := *p
	return &cp, nil
}

func (m *Memory) DeletePeer(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.peers[id]; !ok {
		return ErrPeerNotFound
	}
	delete(m.peers, id)
	return nil
}

func (m *Memory) ArticlesForGroup(_ context.Context, group string) ([]StoredArticle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[group]
	if !ok {
		return nil, nil
	}
	out := make([]StoredArticle, 0, len(g.nums))
	for _, n := range g.nums {
		a := m.byNum[group][n]
		cp := a.StoredArticle
		cp.Num = n
		out = append(out, cp)
	}
	return out, nil
}

func (m *Memory) ListSubscriptions(_ context.Context, userID int64) ([]Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm := m.subs[userID]
	out := make([]Subscription, 0, len(sm))
	for name, at := range sm {
		s := Subscription{GroupName: name, SubscribedAt: at}
		if g, ok := m.groups[name]; ok {
			s.Description = g.Description
			s.Status = g.Status
			s.Low = g.Low
			s.High = g.High
			s.Count = g.Count
		}
		if rm := m.reads[userID]; rm != nil {
			s.LastReadNum = rm[name]
		}
		if s.High > s.LastReadNum {
			s.Unread = s.High - s.LastReadNum
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GroupName < out[j].GroupName })
	return out, nil
}

func (m *Memory) Subscribe(_ context.Context, userID int64, group string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	group = strings.TrimSpace(group)
	if group == "" {
		return errors.New("group required")
	}
	if _, ok := m.groups[group]; !ok {
		return ErrNoGroup
	}
	if m.subs[userID] == nil {
		m.subs[userID] = map[string]time.Time{}
	}
	if _, ok := m.subs[userID][group]; !ok {
		m.subs[userID][group] = time.Now().UTC()
	}
	return nil
}

func (m *Memory) Unsubscribe(_ context.Context, userID int64, group string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sm := m.subs[userID]; sm != nil {
		delete(sm, strings.TrimSpace(group))
	}
	return nil
}

func (m *Memory) GetReadState(_ context.Context, userID int64, group string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rm := m.reads[userID]; rm != nil {
		return rm[strings.TrimSpace(group)], nil
	}
	return 0, nil
}

func (m *Memory) SetReadState(_ context.Context, userID int64, group string, lastReadNum int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lastReadNum < 0 {
		lastReadNum = 0
	}
	if m.reads[userID] == nil {
		m.reads[userID] = map[string]int64{}
	}
	group = strings.TrimSpace(group)
	if cur, ok := m.reads[userID][group]; !ok || lastReadNum > cur {
		m.reads[userID][group] = lastReadNum
	}
	return nil
}

func (m *Memory) RememberMessageID(_ context.Context, msgid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.history[msgid]; !ok {
		m.history[msgid] = time.Now()
	}
	return nil
}

func (m *Memory) CancelMessageID(_ context.Context, msgid string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history[msgid] = time.Now()
	art, ok := m.arts[msgid]
	if !ok {
		return false, nil
	}
	for g, n := range art.groups {
		delete(m.byNum[g], n)
		if mg, ok := m.groups[g]; ok {
			mg.Count--
			if mg.Count < 0 {
				mg.Count = 0
			}
			var nums []int64
			for _, x := range mg.nums {
				if x != n {
					nums = append(nums, x)
				}
			}
			mg.nums = nums
			if len(nums) == 0 {
				mg.Low = mg.High
			} else {
				mg.Low = nums[0]
				for _, x := range nums {
					if x < mg.Low {
						mg.Low = x
					}
				}
			}
		}
	}
	delete(m.arts, msgid)
	var kept []FeedQueueItem
	for _, it := range m.feedQ {
		if it.MessageID != msgid {
			kept = append(kept, it)
		}
	}
	m.feedQ = kept
	return true, nil
}

func (m *Memory) DeleteGroup(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[name]; !ok {
		return ErrNoGroup
	}
	for msgid, art := range m.arts {
		if _, in := art.groups[name]; !in {
			continue
		}
		delete(art.groups, name)
		if len(art.groups) == 0 {
			delete(m.arts, msgid)
		}
	}
	delete(m.groups, name)
	delete(m.byNum, name)
	for uid := range m.subs {
		delete(m.subs[uid], name)
	}
	for uid := range m.reads {
		delete(m.reads[uid], name)
	}
	return nil
}

func (m *Memory) EnqueueFeed(_ context.Context, peerID int64, msgid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range m.feedQ {
		if it.PeerID == peerID && it.MessageID == msgid {
			return nil
		}
	}
	m.nextFQID++
	m.feedQ = append(m.feedQ, FeedQueueItem{
		ID: m.nextFQID, PeerID: peerID, MessageID: msgid,
		NextAttempt: time.Now(), CreatedAt: time.Now(),
	})
	return nil
}

func (m *Memory) ClaimFeedDue(_ context.Context, limit int) ([]FeedQueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 {
		limit = 32
	}
	now := time.Now()
	var out []FeedQueueItem
	for i := range m.feedQ {
		if len(out) >= limit {
			break
		}
		if m.feedQ[i].NextAttempt.After(now) {
			continue
		}
		m.feedQ[i].Attempts++
		m.feedQ[i].NextAttempt = now.Add(2 * time.Minute)
		out = append(out, m.feedQ[i])
	}
	return out, nil
}

func (m *Memory) CompleteFeed(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []FeedQueueItem
	for _, it := range m.feedQ {
		if it.ID != id {
			kept = append(kept, it)
		}
	}
	m.feedQ = kept
	return nil
}

func (m *Memory) FailFeed(_ context.Context, id int64, errMsg string, retryAfter time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if retryAfter <= 0 {
		retryAfter = 2 * time.Minute
	}
	for i := range m.feedQ {
		if m.feedQ[i].ID == id {
			m.feedQ[i].LastError = errMsg
			m.feedQ[i].NextAttempt = time.Now().Add(retryAfter)
			break
		}
	}
	return nil
}

func (m *Memory) FlushFeedQueue(_ context.Context, peerID int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []FeedQueueItem
	n := 0
	for _, it := range m.feedQ {
		if peerID > 0 && it.PeerID != peerID {
			kept = append(kept, it)
			continue
		}
		n++
	}
	m.feedQ = kept
	return n, nil
}

func (m *Memory) FeedQueueStats(_ context.Context) (FeedQueueStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := FeedQueueStats{ByPeer: map[int64]int{}}
	var oldest time.Time
	for _, it := range m.feedQ {
		st.ByPeer[it.PeerID]++
		st.Depth++
		if oldest.IsZero() || it.CreatedAt.Before(oldest) {
			oldest = it.CreatedAt
		}
	}
	if !oldest.IsZero() {
		st.OldestAge = time.Since(oldest)
	}
	return st, nil
}

var (
	ErrDuplicate = errStore("duplicate message-id")
	ErrNoGroup   = errStore("no such newsgroup")
)

type errStore string

func (e errStore) Error() string { return string(e) }
