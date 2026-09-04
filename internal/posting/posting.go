package posting

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"openusenet/internal/archive"
	"openusenet/internal/article"
	"openusenet/internal/binary"
	"openusenet/internal/config"
	"openusenet/internal/retention"
	"openusenet/internal/store"
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

// Accept validates, injects, stores, appends mbox, and offers to peers.
func Accept(ctx context.Context, cfg config.Config, st store.Store, mbox *archive.MBox, feeder Feeder, lg *log.Logger, in Input, userID int64) (*store.PostResult, error) {
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
		art.Bytes(), art.Lines(), art.Newsgroups())
	if err != nil {
		return nil, err
	}
	if _, err := st.NoteAccept(ctx, art.Newsgroups(), isBin, retention.FloodFromConfig(cfg)); err != nil {
		lg.Printf("retention note: %v", err)
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
	return res, nil
}
