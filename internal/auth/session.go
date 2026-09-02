package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/openusenet/openusenet/internal/store"
)

const SessionCookie = "ous_session"
const SessionTTL = 24 * time.Hour

type Sessions struct {
	mu   sync.Mutex
	byID map[string]session
}

type session struct {
	user   store.User
	expiry time.Time
}

func NewSessions() *Sessions {
	return &Sessions{byID: map[string]session{}}
}

func (s *Sessions) Create(u store.User) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[id] = session{user: u, expiry: time.Now().Add(SessionTTL)}
	return id, nil
}

func (s *Sessions) Get(id string) (store.User, bool) {
	if id == "" {
		return store.User{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.byID[id]
	if !ok || time.Now().After(ss.expiry) {
		if ok {
			delete(s.byID, id)
		}
		return store.User{}, false
	}
	ss.expiry = time.Now().Add(SessionTTL)
	s.byID[id] = ss
	return ss.user, true
}

func (s *Sessions) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
}
