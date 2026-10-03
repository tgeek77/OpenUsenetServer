package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const SessionCookie = "ous_session"
const SessionTTL = 24 * time.Hour
const RememberTTL = 30 * 24 * time.Hour

// Token is the signed session carried in the ous_session cookie.
type Token struct {
	UserID   int64
	Gen      int
	Expiry   time.Time
	Remember bool
}

// Signer seals and opens session cookies. The key is stored in the database
// so a process restart does not log everyone out.
type Signer struct {
	key []byte
}

func NewSigner(key []byte) *Signer {
	k := make([]byte, len(key))
	copy(k, key)
	return &Signer{key: k}
}

func (s *Signer) Seal(userID int64, gen int, exp time.Time, remember bool) (string, error) {
	if s == nil || len(s.key) == 0 {
		return "", errors.New("session signer is not configured")
	}
	rm := 0
	if remember {
		rm = 1
	}
	payload := strconv.FormatInt(userID, 10) + "." + strconv.Itoa(gen) + "." + strconv.FormatInt(exp.Unix(), 10) + "." + strconv.Itoa(rm)
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(payload))
	sum := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sum), nil
}

func (s *Signer) Open(raw string) (Token, bool) {
	if s == nil || len(s.key) == 0 || raw == "" {
		return Token{}, false
	}
	body, sig, ok := strings.Cut(raw, ".")
	if !ok || body == "" || sig == "" {
		return Token{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Token{}, false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return Token{}, false
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return Token{}, false
	}
	parts := strings.Split(string(payload), ".")
	if len(parts) != 4 {
		return Token{}, false
	}
	uid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || uid <= 0 {
		return Token{}, false
	}
	gen, err := strconv.Atoi(parts[1])
	if err != nil || gen < 0 {
		return Token{}, false
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return Token{}, false
	}
	exp := time.Unix(expUnix, 0)
	if time.Now().After(exp) {
		return Token{}, false
	}
	return Token{UserID: uid, Gen: gen, Expiry: exp, Remember: parts[3] == "1"}, true
}
