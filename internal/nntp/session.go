package nntp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/openusenet/openusenet/internal/archive"
	"github.com/openusenet/openusenet/internal/article"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/store"
	"github.com/openusenet/openusenet/internal/wildmat"
)

type Session struct {
	conn   *Conn
	store  store.Store
	mbox   *archive.MBox
	cfg    config.Config
	group  *store.Group
	cur    int64
	closed bool
	log    *log.Logger
	feeder Feeder
}

// Feeder is an outbound IHAVE client. Tests pass nil.
type Feeder interface {
	Offer(msgid, path string, groups []string, wire []byte)
}

func Serve(conn *Conn, st store.Store, mbox *archive.MBox, cfg config.Config, lg *log.Logger, feeder Feeder) {
	if lg == nil {
		lg = log.Default()
	}
	s := &Session{conn: conn, store: st, mbox: mbox, cfg: cfg, log: lg, feeder: feeder}
	defer conn.Close()
	if err := conn.Reply(OKBannerPost, Software+" "+Version+" posting allowed"); err != nil {
		return
	}
	for !s.closed {
		line, err := conn.ReadCommand()
		if err != nil {
			if errors.Is(err, io.EOF) || IsTimeout(err) {
				return
			}
			if IsTooLong(err) {
				_ = conn.Reply(ErrSyntax, "command line too long")
				continue
			}
			return
		}
		if err := s.dispatch(line); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			s.log.Printf("nntp %s: %v", conn.Remote(), err)
			_ = conn.Reply(FailAction, "internal error")
		}
	}
}

type cmd struct {
	min, max int
	fn       func(*Session, []string) error
}

func (s *Session) dispatch(line string) error {
	args := SplitArgs(line)
	if len(args) == 0 {
		return s.conn.Reply(ErrSyntax, "empty command")
	}
	name := strings.ToUpper(args[0])
	spec, ok := commands[name]
	if !ok {
		return s.conn.Reply(ErrCommand, "unknown command")
	}
	narg := len(args) // including verb
	if narg < spec.min || (spec.max >= 0 && narg > spec.max) {
		return s.conn.Reply(ErrSyntax, "syntax error")
	}
	return spec.fn(s, args)
}

var commands = map[string]cmd{
	"CAPABILITIES": {1, 2, cmdCapabilities},
	"HELP":         {1, 1, cmdHelp},
	"QUIT":         {1, 1, cmdQuit},
	"DATE":         {1, 1, cmdDate},
	"MODE":         {2, 2, cmdMode},
	"GROUP":        {2, 2, cmdGroup},
	"LISTGROUP":    {1, 3, cmdListgroup},
	"LIST":         {1, 3, cmdList},
	"ARTICLE":      {1, 2, cmdArticle},
	"HEAD":         {1, 2, cmdHead},
	"BODY":         {1, 2, cmdBody},
	"STAT":         {1, 2, cmdStat},
	"NEXT":         {1, 1, cmdNext},
	"LAST":         {1, 1, cmdLast},
	"OVER":         {1, 2, cmdOver},
	"XOVER":        {1, 2, cmdOver},
	"HDR":          {2, 3, cmdHdr},
	"XHDR":         {2, 3, cmdHdr},
	"POST":         {1, 1, cmdPost},
	"IHAVE":        {2, 2, cmdIHave},
	"NEWNEWS":      {4, 5, cmdNewnews},
	"NEWGROUPS":    {3, 4, cmdNewgroups},
	"SLAVE":        {1, 1, cmdSlave},
}

func cmdSlave(s *Session, _ []string) error {
	return s.conn.Reply(ErrCommand, "SLAVE was removed in RFC 3977")
}

func cmdCapabilities(s *Session, _ []string) error {
	lines := []string{
		"VERSION 2",
		"READER",
		"POST",
		"NEWNEWS",
		"HDR",
		"OVER MSGID",
		"IHAVE",
		"LIST ACTIVE NEWSGROUPS ACTIVE.TIMES OVERVIEW.FMT HEADERS",
		"IMPLEMENTATION " + Software + " " + Version,
	}
	var body []byte
	for _, l := range lines {
		body = append(body, l...)
		body = append(body, '\r', '\n')
	}
	return s.conn.WriteBlock(InfoCapabilities, nil, "Capability list:", body)
}

func cmdHelp(s *Session, _ []string) error {
	text := "  ARTICLE [number|<message-id>]\r\n" +
		"  BODY [number|<message-id>]\r\n" +
		"  CAPABILITIES [keyword]\r\n" +
		"  DATE\r\n" +
		"  GROUP newsgroup\r\n" +
		"  HDR header [range|<message-id>]\r\n" +
		"  HEAD [number|<message-id>]\r\n" +
		"  HELP\r\n" +
		"  IHAVE <message-id>\r\n" +
		"  LAST\r\n" +
		"  LIST [ACTIVE [wildmat]|NEWSGROUPS [wildmat]|ACTIVE.TIMES [wildmat]|OVERVIEW.FMT|HEADERS]\r\n" +
		"  LISTGROUP [newsgroup [range]]\r\n" +
		"  MODE READER\r\n" +
		"  NEWGROUPS [yy]yymmdd hhmmss [GMT]\r\n" +
		"  NEWNEWS wildmat [yy]yymmdd hhmmss [GMT]\r\n" +
		"  NEXT\r\n" +
		"  OVER [range]\r\n" +
		"  POST\r\n" +
		"  QUIT\r\n" +
		"  STAT [number|<message-id>]\r\n"
	return s.conn.WriteBlock(InfoHelp, nil, "help text follows", []byte(text))
}

func cmdQuit(s *Session, _ []string) error {
	s.closed = true
	return s.conn.Reply(OKQuit, "NNTP Service exits normally")
}

func cmdDate(s *Session, _ []string) error {
	t := time.Now().UTC().Format("20060102150405")
	return s.conn.ReplyArgs(InfoDate, []string{t}, "server date and time")
}

func cmdMode(s *Session, args []string) error {
	if !strings.EqualFold(args[1], "READER") {
		return s.conn.Reply(ErrSyntax, "unknown MODE option")
	}
	return s.conn.Reply(OKBannerPost, "Reader mode, posting permitted")
}

func cmdGroup(s *Session, args []string) error {
	g, err := s.store.GetGroup(context.Background(), args[1])
	if err != nil {
		return err
	}
	if g == nil {
		return s.conn.Reply(FailBadGroup, "no such newsgroup")
	}
	s.group = g
	s.cur = firstNum(g)
	return s.conn.ReplyArgs(OKGroup, []string{
		itoa64(g.Count), itoa64(g.Low), itoa64(g.High), g.Name,
	}, "group selected")
}

func firstNum(g *store.Group) int64 {
	if g.Low > 0 {
		return g.Low
	}
	return 0
}

func cmdListgroup(s *Session, args []string) error {
	name := ""
	rangeSpec := ""
	if len(args) >= 2 {
		name = args[1]
	}
	if len(args) >= 3 {
		rangeSpec = args[2]
	}
	if name == "" {
		if s.group == nil {
			return s.conn.Reply(FailNoGroup, "no newsgroup selected")
		}
		name = s.group.Name
	}
	g, err := s.store.GetGroup(context.Background(), name)
	if err != nil {
		return err
	}
	if g == nil {
		return s.conn.Reply(FailBadGroup, "no such newsgroup")
	}
	s.group = g
	s.cur = firstNum(g)
	lo, hi, err := parseRange(rangeSpec)
	if err != nil {
		return s.conn.Reply(ErrSyntax, "syntax error")
	}
	nums, err := s.store.ArticleNumbers(context.Background(), name, lo, hi)
	if err != nil {
		return err
	}
	var body []byte
	for _, n := range nums {
		body = append(body, []byte(itoa64(n))...)
		body = append(body, '\r', '\n')
	}
	return s.conn.WriteBlock(OKGroup, []string{
		itoa64(g.Count), itoa64(g.Low), itoa64(g.High), g.Name,
	}, "article numbers follow", body)
}

func cmdList(s *Session, args []string) error {
	keyword := "ACTIVE"
	wild := ""
	if len(args) >= 2 {
		keyword = strings.ToUpper(args[1])
	}
	if len(args) >= 3 {
		wild = args[2]
		if !wildmat.Valid(wild) {
			return s.conn.Reply(ErrSyntax, "syntax error")
		}
	}
	ctx := context.Background()
	switch keyword {
	case "ACTIVE":
		gs, err := s.store.ListGroups(ctx, wild)
		if err != nil {
			return err
		}
		var body []byte
		for _, g := range gs {
			line := fmt.Sprintf("%s %d %d %s\r\n", g.Name, g.High, g.Low, g.Status)
			body = append(body, line...)
		}
		return s.conn.WriteBlock(OKList, nil, "list of newsgroups follows", body)
	case "NEWSGROUPS":
		gs, err := s.store.ListGroups(ctx, wild)
		if err != nil {
			return err
		}
		var body []byte
		for _, g := range gs {
			line := fmt.Sprintf("%s\t%s\r\n", g.Name, g.Description)
			body = append(body, line...)
		}
		return s.conn.WriteBlock(OKList, nil, "list of newsgroups follows", body)
	case "ACTIVE.TIMES":
		gs, err := s.store.ListGroups(ctx, wild)
		if err != nil {
			return err
		}
		var body []byte
		for _, g := range gs {
			line := fmt.Sprintf("%s %d %s\r\n", g.Name, g.CreatedAt.Unix(), "openusenet")
			body = append(body, line...)
		}
		return s.conn.WriteBlock(OKList, nil, "list of newsgroups follows", body)
	case "OVERVIEW.FMT":
		body := []byte("Subject:\r\nFrom:\r\nDate:\r\nMessage-ID:\r\nReferences:\r\n:bytes\r\n:lines\r\nXref:full\r\n")
		return s.conn.WriteBlock(OKList, nil, "Overview format:", body)
	case "HEADERS":
		body := []byte(":\r\n:bytes\r\n:lines\r\n")
		return s.conn.WriteBlock(OKList, nil, "Headers list follows", body)
	default:
		return s.conn.Reply(ErrSyntax, "unknown LIST keyword")
	}
}

func cmdArticle(s *Session, args []string) error {
	return s.fetch(args, OKArticle, true, true)
}
func cmdHead(s *Session, args []string) error {
	return s.fetch(args, OKHead, true, false)
}
func cmdBody(s *Session, args []string) error {
	return s.fetch(args, OKBody, false, true)
}
func cmdStat(s *Session, args []string) error {
	return s.fetch(args, OKStat, false, false)
}

func (s *Session) fetch(args []string, code int, head, body bool) error {
	a, byMsgID, err := s.lookup(args)
	if err != nil {
		return err
	}
	if a == nil {
		return nil // lookup already replied
	}
	nstr := itoa64(a.Num)
	if byMsgID {
		nstr = "0"
	}
	text := "article follows"
	switch code {
	case OKHead:
		text = "headers follow"
	case OKBody:
		text = "body follows"
	case OKStat:
		text = "article exists"
	}
	if code == OKStat {
		return s.conn.ReplyArgs(code, []string{nstr, a.MessageID}, text)
	}
	var payload []byte
	if head && body {
		payload = []byte(a.Headers + "\r\n\r\n" + a.Body)
	} else if head {
		payload = []byte(a.Headers + "\r\n")
	} else {
		payload = []byte(a.Body)
	}
	return s.conn.WriteBlock(code, []string{nstr, a.MessageID}, text, payload)
}

func (s *Session) lookup(args []string) (*store.StoredArticle, bool, error) {
	ctx := context.Background()
	if len(args) == 1 {
		if s.group == nil {
			_ = s.conn.Reply(FailNoGroup, "no newsgroup selected")
			return nil, false, nil
		}
		if s.cur <= 0 {
			_ = s.conn.Reply(FailArtnumInvalid, "current article number is invalid")
			return nil, false, nil
		}
		a, err := s.store.GetByNumber(ctx, s.group.Name, s.cur)
		if err != nil {
			return nil, false, err
		}
		if a == nil {
			_ = s.conn.Reply(FailArtnumNotFound, "no article with that number")
			return nil, false, nil
		}
		return a, false, nil
	}
	arg := args[1]
	if strings.HasPrefix(arg, "<") {
		if !article.ValidMessageID(arg) {
			_ = s.conn.Reply(ErrSyntax, "syntax error")
			return nil, true, nil
		}
		a, err := s.store.GetByMsgID(ctx, arg)
		if err != nil {
			return nil, true, err
		}
		if a == nil {
			_ = s.conn.Reply(FailMsgidNotFound, "no article with that message-id")
			return nil, true, nil
		}
		return a, true, nil
	}
	if s.group == nil {
		_ = s.conn.Reply(FailNoGroup, "no newsgroup selected")
		return nil, false, nil
	}
	n, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || n < 1 {
		_ = s.conn.Reply(ErrSyntax, "syntax error")
		return nil, false, nil
	}
	a, err := s.store.GetByNumber(ctx, s.group.Name, n)
	if err != nil {
		return nil, false, err
	}
	if a == nil {
		_ = s.conn.Reply(FailArtnumNotFound, "no article with that number")
		return nil, false, nil
	}
	s.cur = n
	return a, false, nil
}

func cmdNext(s *Session, _ []string) error {
	if s.group == nil {
		return s.conn.Reply(FailNoGroup, "no newsgroup selected")
	}
	if s.cur <= 0 {
		return s.conn.Reply(FailArtnumInvalid, "current article number is invalid")
	}
	a, err := s.store.Next(context.Background(), s.group.Name, s.cur)
	if err != nil {
		return err
	}
	if a == nil {
		return s.conn.Reply(FailNext, "no next article in this group")
	}
	s.cur = a.Num
	return s.conn.ReplyArgs(OKStat, []string{itoa64(a.Num), a.MessageID}, "article retrieved")
}

func cmdLast(s *Session, _ []string) error {
	if s.group == nil {
		return s.conn.Reply(FailNoGroup, "no newsgroup selected")
	}
	if s.cur <= 0 {
		return s.conn.Reply(FailArtnumInvalid, "current article number is invalid")
	}
	a, err := s.store.Prev(context.Background(), s.group.Name, s.cur)
	if err != nil {
		return err
	}
	if a == nil {
		return s.conn.Reply(FailPrev, "no previous article in this group")
	}
	s.cur = a.Num
	return s.conn.ReplyArgs(OKStat, []string{itoa64(a.Num), a.MessageID}, "article retrieved")
}

func cmdOver(s *Session, args []string) error {
	if s.group == nil && (len(args) < 2 || !strings.HasPrefix(args[1], "<")) {
		return s.conn.Reply(FailNoGroup, "no newsgroup selected")
	}
	if len(args) == 2 && strings.HasPrefix(args[1], "<") {
		a, err := s.store.GetByMsgID(context.Background(), args[1])
		if err != nil {
			return err
		}
		if a == nil {
			return s.conn.Reply(FailMsgidNotFound, "no article with that message-id")
		}
		line := article.OverviewLine(0, &article.Article{
			Headers: map[string][]string{
				"Subject": {a.Subject}, "From": {a.From}, "Date": {a.Date},
				"Message-ID": {a.MessageID}, "References": {a.Refs},
			},
			Body: a.Body,
		}, a.Xref)
		return s.conn.WriteBlock(OKOver, nil, "Overview information follows", []byte(line+"\r\n"))
	}
	lo, hi := s.cur, s.cur
	if len(args) == 2 {
		var err error
		lo, hi, err = parseRange(args[1])
		if err != nil {
			return s.conn.Reply(ErrSyntax, "syntax error")
		}
		if lo == 0 && hi == 0 {
			lo, hi = s.cur, s.cur
		}
	}
	if lo <= 0 {
		return s.conn.Reply(FailArtnumInvalid, "current article number is invalid")
	}
	rows, err := s.store.Overview(context.Background(), s.group.Name, lo, hi)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return s.conn.Reply(FailArtnumNotFound, "no articles in that range")
	}
	var body []byte
	for _, r := range rows {
		line := fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t%d\t%d\tXref:full %s\r\n",
			r.Num, r.Subject, r.From, r.Date, r.MsgID, r.Refs, r.Bytes, r.Lines, r.Xref)
		body = append(body, line...)
	}
	return s.conn.WriteBlock(OKOver, nil, "Overview information follows", body)
}

func cmdHdr(s *Session, args []string) error {
	header := args[1]
	if s.group == nil && (len(args) < 3 || !strings.HasPrefix(args[2], "<")) {
		return s.conn.Reply(FailNoGroup, "no newsgroup selected")
	}
	if len(args) == 3 && strings.HasPrefix(args[2], "<") {
		a, err := s.store.GetByMsgID(context.Background(), args[2])
		if err != nil {
			return err
		}
		if a == nil {
			return s.conn.Reply(FailMsgidNotFound, "no article with that message-id")
		}
		val := hdrFromStored(header, a)
		line := fmt.Sprintf("0 %s\r\n", val)
		return s.conn.WriteBlock(OKHdr, nil, "headers follow", []byte(line))
	}
	lo, hi := s.cur, s.cur
	if len(args) == 3 {
		var err error
		lo, hi, err = parseRange(args[2])
		if err != nil {
			return s.conn.Reply(ErrSyntax, "syntax error")
		}
	}
	if s.group == nil {
		return s.conn.Reply(FailNoGroup, "no newsgroup selected")
	}
	rows, err := s.store.Header(context.Background(), s.group.Name, lo, hi, header)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return s.conn.Reply(FailArtnumNotFound, "no articles in that range")
	}
	var body []byte
	for _, r := range rows {
		line := fmt.Sprintf("%d %s\r\n", r.Num, r.Header)
		body = append(body, line...)
	}
	return s.conn.WriteBlock(OKHdr, nil, "headers follow", body)
}

func hdrFromStored(header string, a *store.StoredArticle) string {
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
		return fmt.Sprintf("%d", a.Bytes)
	case ":lines":
		return fmt.Sprintf("%d", a.Lines)
	case "xref":
		return a.Xref
	default:
		for _, line := range strings.Split(a.Headers, "\r\n") {
			k, v, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(k, header) {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
}

func cmdPost(s *Session, _ []string) error {
	if err := s.conn.Reply(ContPost, "send article to be posted"); err != nil {
		return err
	}
	raw, err := s.conn.ReadArticle(s.cfg.Limits.MaxArtSize)
	if err != nil {
		if IsTooLong(err) {
			return s.conn.Reply(FailPostReject, "article too large")
		}
		return err
	}
	art, err := article.Parse(raw)
	if err != nil {
		return s.conn.Reply(FailPostReject, err.Error())
	}
	if err := article.InjectForPost(art, article.InjectOpts{
		Pathhost:     s.cfg.Server.Pathhost,
		Hostname:     s.cfg.Server.Hostname,
		Organization: s.cfg.Server.Organization,
	}); err != nil {
		return s.conn.Reply(FailPostReject, err.Error())
	}
	ctx := context.Background()
	dup, err := s.store.HasMessageID(ctx, art.Get("Message-ID"))
	if err != nil {
		return err
	}
	if dup {
		return s.conn.Reply(FailPostReject, "duplicate Message-ID")
	}
	wire := art.Wire()
	if _, err := s.storeArticle(ctx, art, wire); errors.Is(err, store.ErrNoGroup) {
		return s.conn.Reply(FailPostReject, "newsgroup does not exist")
	} else if errors.Is(err, store.ErrDuplicate) {
		return s.conn.Reply(FailPostReject, "duplicate Message-ID")
	} else if err != nil {
		return err
	}
	if err := s.conn.Reply(OKPost, "article received "+art.Get("Message-ID")); err != nil {
		return err
	}
	s.offer(art, wire)
	return nil
}

func cmdIHave(s *Session, args []string) error {
	msgid := args[1]
	if !article.ValidMessageID(msgid) {
		return s.conn.Reply(ErrSyntax, "syntax error")
	}
	ctx := context.Background()
	dup, err := s.store.HasMessageID(ctx, msgid)
	if err != nil {
		return s.conn.Reply(FailIHaveDefer, "try again later")
	}
	if dup {
		return s.conn.Reply(FailIHaveRefuse, "article not wanted")
	}
	if err := s.conn.Reply(ContIHave, "send article to be transferred"); err != nil {
		return err
	}
	raw, err := s.conn.ReadArticle(s.cfg.Limits.MaxArtSize)
	if err != nil {
		if IsTooLong(err) {
			return s.conn.Reply(FailIHaveReject, "article too large")
		}
		return err
	}
	art, err := article.Parse(raw)
	if err != nil {
		return s.conn.Reply(FailIHaveReject, err.Error())
	}
	if err := article.InjectForIHave(art, article.InjectOpts{
		Pathhost: s.cfg.Server.Pathhost,
		Hostname: s.cfg.Server.Hostname,
	}); err != nil {
		return s.conn.Reply(FailIHaveReject, err.Error())
	}
	if !strings.EqualFold(art.Get("Message-ID"), msgid) {
		return s.conn.Reply(FailIHaveReject, "Message-ID does not match")
	}
	dup, err = s.store.HasMessageID(ctx, msgid)
	if err != nil {
		return s.conn.Reply(FailIHaveDefer, "try again later")
	}
	if dup {
		return s.conn.Reply(FailIHaveReject, "duplicate Message-ID")
	}
	wire := art.Wire()
	if _, err := s.storeArticle(ctx, art, wire); errors.Is(err, store.ErrNoGroup) {
		return s.conn.Reply(FailIHaveReject, "newsgroup does not exist")
	} else if errors.Is(err, store.ErrDuplicate) {
		return s.conn.Reply(FailIHaveReject, "duplicate Message-ID")
	} else if err != nil {
		return err
	}
	if err := s.conn.Reply(OKIHave, "article transferred "+msgid); err != nil {
		return err
	}
	s.offer(art, wire)
	return nil
}

func (s *Session) storeArticle(ctx context.Context, art *article.Article, wire []byte) (*store.PostResult, error) {
	hdr, body, _ := strings.Cut(string(wire), "\r\n\r\n")
	res, err := s.store.Post(ctx, hdr, body, art.Get("Message-ID"), art.Get("Subject"),
		art.Get("From"), art.Get("Date"), art.Get("References"), s.cfg.Server.Hostname,
		art.Bytes(), art.Lines(), art.Newsgroups())
	if err != nil {
		return nil, err
	}
	if s.mbox != nil {
		art.Set("Xref", res.Xref)
		for g := range res.Numbers {
			if _, _, err := s.mbox.Append(g, art); err != nil {
				s.log.Printf("mbox append %s: %v", g, err)
			}
		}
	}
	return res, nil
}

func (s *Session) offer(art *article.Article, wire []byte) {
	if s.feeder == nil {
		return
	}
	s.feeder.Offer(art.Get("Message-ID"), art.Get("Path"), art.Newsgroups(), wire)
}

func cmdNewnews(s *Session, args []string) error {
	wild := args[1]
	if !wildmat.Valid(wild) {
		return s.conn.Reply(ErrSyntax, "syntax error")
	}
	t, err := parseNNTPTime(args)
	if err != nil {
		return s.conn.Reply(ErrSyntax, "syntax error")
	}
	ids, err := s.store.NewNews(context.Background(), wild, t)
	if err != nil {
		return err
	}
	var body []byte
	for _, id := range ids {
		body = append(body, id...)
		body = append(body, '\r', '\n')
	}
	return s.conn.WriteBlock(OKNewNews, nil, "list of new articles follows", body)
}

func cmdNewgroups(s *Session, args []string) error {
	t, err := parseNNTPTime(args)
	if err != nil {
		return s.conn.Reply(ErrSyntax, "syntax error")
	}
	gs, err := s.store.NewGroups(context.Background(), t)
	if err != nil {
		return err
	}
	var body []byte
	for _, g := range gs {
		line := fmt.Sprintf("%s %d %d %s\r\n", g.Name, g.High, g.Low, g.Status)
		body = append(body, line...)
	}
	return s.conn.WriteBlock(OKNewGroups, nil, "list of new newsgroups follows", body)
}

func parseNNTPTime(args []string) (time.Time, error) {
	// DATE and NEWNEWS/NEWGROUPS: verb wildmat? yymmdd hhmmss [GMT]
	// find two tokens that look like date time.
	var date, tod string
	gmt := false
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.EqualFold(a, "GMT") {
			gmt = true
			continue
		}
		if len(a) == 6 || len(a) == 8 {
			if date == "" && isDigits(a) {
				date = a
				continue
			}
		}
		if len(a) == 6 && isDigits(a) && date != "" {
			tod = a
		}
	}
	if date == "" || tod == "" {
		return time.Time{}, fmt.Errorf("bad time")
	}
	layout := "060102150405"
	if len(date) == 8 {
		layout = "20060102150405"
	}
	t, err := time.Parse(layout, date+tod)
	if err != nil {
		return time.Time{}, err
	}
	if gmt {
		t = t.UTC()
	}
	return t, nil
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseRange(spec string) (lo, hi int64, err error) {
	if spec == "" {
		return 0, 0, nil
	}
	if strings.Contains(spec, "-") {
		a, b, _ := strings.Cut(spec, "-")
		lo, err = strconv.ParseInt(a, 10, 64)
		if err != nil || lo < 1 {
			return 0, 0, fmt.Errorf("range")
		}
		if b == "" {
			return lo, MaxArtNum, nil
		}
		hi, err = strconv.ParseInt(b, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		return lo, hi, nil
	}
	n, err := strconv.ParseInt(spec, 10, 64)
	if err != nil || n < 1 {
		return 0, 0, fmt.Errorf("range")
	}
	return n, n, nil
}

func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}
