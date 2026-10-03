package posting

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"openusenet/internal/archive"
	"openusenet/internal/article"
	"openusenet/internal/binary"
	"openusenet/internal/config"
	"openusenet/internal/mail"
	"openusenet/internal/moderate"
	"openusenet/internal/retention"
	"openusenet/internal/store"
	"openusenet/internal/wildmat"
)

// Feeder is the outbound IHAVE interface used after accept.
type Feeder interface {
	Offer(msgid, path string, groups []string, wire []byte)
}

// Input is a web or programmatic post/reply.
type Input struct {
	Newsgroups string `json:"newsgroups"`
	Subject    string `json:"subject"`
	From       string `json:"from"`
	Body       string `json:"body"`
	References string `json:"references"`
	ReplyToID  string `json:"reply_to_msgid"`
}

// Outcome is a stored article or a moderated submission that was mailed.
type Outcome struct {
	Stored *store.PostResult
	Mailed *Mailed
}

// Mailed is a local post sent to a moderator and not stored.
type Mailed struct {
	Group     string
	Address   string
	MessageID string
}

// Accept validates and injects a local post. An unapproved article for a
// moderated group is mailed and not stored. Anything else is stored, appended
// to the mbox, and offered to peers.
func Accept(ctx context.Context, cfg config.Config, st store.Store, mbox *archive.MBox, feeder Feeder, lg *log.Logger, in Input, userID int64) (*Outcome, error) {
	if lg == nil {
		lg = log.Default()
	}
	from := strings.TrimSpace(in.From)
	subject := strings.TrimSpace(in.Subject)
	groups := strings.TrimSpace(in.Newsgroups)
	if from == "" || subject == "" || groups == "" {
		return nil, fmt.Errorf("from, subject, and newsgroups are required")
	}
	refs := strings.TrimSpace(in.References)
	if reply := strings.TrimSpace(in.ReplyToID); reply != "" {
		if refs == "" {
			refs = reply
		} else if !strings.Contains(refs, reply) {
			refs = refs + " " + reply
		}
	}
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("Newsgroups: " + groups + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	if refs != "" {
		b.WriteString("References: " + refs + "\r\n")
		if reply := strings.TrimSpace(in.ReplyToID); reply != "" {
			b.WriteString("In-Reply-To: " + reply + "\r\n")
		}
	}
	b.WriteString("\r\n")
	body := in.Body
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\r\n") {
		b.WriteString("\r\n")
	}

	art, err := article.Parse([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	if err := article.InjectForPost(art, article.InjectOpts{
		Pathhost:     cfg.Server.Pathhost,
		Hostname:     cfg.Server.Hostname,
		Organization: cfg.Server.Organization,
	}); err != nil {
		return nil, err
	}
	patterns, err := st.ListGroupBans(ctx)
	if err != nil {
		return nil, err
	}
	if len(patterns) > 0 {
		kept := wildmat.KeepGroups(patterns, art.Newsgroups())
		if len(kept) == 0 {
			return nil, fmt.Errorf("newsgroups are not accepted")
		}
		if len(kept) != len(art.Newsgroups()) {
			art.Set("Newsgroups", strings.Join(kept, ","))
		}
	}
	if article.TooOld(art.Get("Date"), cfg.Limits.ArtCutoffDays, time.Time{}) {
		if cfg.Limits.RememberRejects {
			_ = st.RememberMessageID(ctx, art.Get("Message-ID"))
		}
		return nil, fmt.Errorf("article too old")
	}
	if mailed, err := mailModerated(ctx, cfg, st, lg, art); err != nil || mailed != nil {
		if err != nil {
			return nil, err
		}
		return &Outcome{Mailed: mailed}, nil
	}
	isBin := binary.LooksBinary(art.RawHeaders, art.Body)
	if isBin && userID > 0 {
		limit := cfg.Retention.UserBinaryPostsPerDay
		if limit > 0 {
			if _, err := st.ConsumeBinaryPostQuota(ctx, userID, limit); err != nil {
				if errors.Is(err, store.ErrQuotaExceeded) {
					return nil, fmt.Errorf("%w (%d/day)", store.ErrQuotaExceeded, limit)
				}
				return nil, err
			}
		}
	}
	msgid := art.Get("Message-ID")
	dup, err := st.HasMessageID(ctx, msgid)
	if err != nil {
		return nil, err
	}
	if dup {
		return nil, store.ErrDuplicate
	}
	wire := art.Wire()
	hdr, bodyPart, _ := strings.Cut(string(wire), "\r\n\r\n")
	res, err := st.Post(ctx, hdr, bodyPart, msgid, art.Get("Subject"),
		art.Get("From"), art.Get("Date"), art.Get("References"), cfg.Server.Hostname,
		art.Bytes(), art.Lines(), art.Newsgroups(), isBin)
	if err != nil {
		return nil, err
	}
	if _, err := st.NoteAccept(ctx, art.Newsgroups(), isBin, retention.FloodFromConfig(cfg)); err != nil {
		lg.Printf("retention note: %v", err)
	}
	if err := st.RecordContentStats(ctx, store.ContentStatsEvent{
		Groups: art.Newsgroups(), From: art.Get("From"), Path: art.Get("Path"),
		Binary: isBin, ExcludeSite: []string{cfg.Server.Pathhost, cfg.Server.Hostname},
	}); err != nil {
		lg.Printf("content stats: %v", err)
	}
	if mbox != nil {
		art.Set("Xref", res.Xref)
		for g := range res.Numbers {
			if _, _, err := mbox.Append(g, art); err != nil {
				lg.Printf("mbox append %s: %v", g, err)
			}
		}
	}
	if feeder != nil {
		feeder.Offer(msgid, art.Get("Path"), art.Newsgroups(), wire)
	}
	return &Outcome{Stored: res}, nil
}

func mailModerated(ctx context.Context, cfg config.Config, st store.Store, lg *log.Logger, art *article.Article) (*Mailed, error) {
	approved := strings.TrimSpace(art.Get("Approved")) != ""
	group, err := moderate.Target(art.Newsgroups(), approved, func(name string) (string, bool, error) {
		g, err := st.GetGroup(ctx, name)
		if err != nil {
			return "", false, err
		}
		if g == nil {
			return "", false, nil
		}
		return g.Status, true, nil
	})
	if err != nil || group == "" {
		return nil, err
	}
	dup, err := st.HasMessageID(ctx, art.Get("Message-ID"))
	if err != nil {
		return nil, err
	}
	if dup {
		return nil, store.ErrDuplicate
	}
	stored, err := st.ListModeratorRules(ctx)
	if err != nil {
		return nil, err
	}
	rules := make([]moderate.Rule, len(stored))
	for i, r := range stored {
		rules[i] = moderate.Rule{Pattern: r.Pattern, Address: r.Address}
	}
	settings, err := mail.Load(ctx, st, cfg)
	if err != nil {
		return nil, err
	}
	addr, err := moderate.Submit(ctx, settings, group, rules, art.Wire())
	if err != nil {
		return nil, err
	}
	lg.Printf("moderated submit group=%s to=%s msgid=%s", group, addr, art.Get("Message-ID"))
	return &Mailed{Group: group, Address: addr, MessageID: art.Get("Message-ID")}, nil
}
