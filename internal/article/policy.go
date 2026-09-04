package article

import (
	"fmt"
	"strings"
	"time"
)

// ParseDate parses common Usenet Date header forms.
func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 MST",
		"20060102150405",
		"2006-01-02T15:04:05Z",
	}
	var last error
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t.UTC(), nil
		}
		last = err
	}
	return time.Time{}, last
}

// TooOld reports whether the article Date is older than cutoffDays.
// cutoffDays <= 0 disables the check. Unparseable dates are not considered too old.
func TooOld(dateHdr string, cutoffDays int, now time.Time) bool {
	if cutoffDays <= 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	t, err := ParseDate(dateHdr)
	if err != nil {
		return false
	}
	return now.Sub(t) > time.Duration(cutoffDays)*24*time.Hour
}

// CancelTarget returns the Message-ID targeted by a Control: cancel header,
// or empty if this is not a cancel control message.
func CancelTarget(a *Article) string {
	ctrl := strings.TrimSpace(a.Get("Control"))
	if ctrl == "" {
		return ""
	}
	fields := strings.Fields(ctrl)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "cancel") {
		return ""
	}
	id := strings.TrimSpace(fields[1])
	if ValidMessageID(id) {
		return id
	}
	return ""
}

// SupersedesTarget returns the Message-ID from a Supersedes header, if valid.
func SupersedesTarget(a *Article) string {
	id := strings.TrimSpace(a.Get("Supersedes"))
	if ValidMessageID(id) {
		return id
	}
	return ""
}
