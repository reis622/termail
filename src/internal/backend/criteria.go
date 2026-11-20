package backend

import (
	"context"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"termail/internal/mailmsg"
	"termail/internal/search"
)

// Criteria is a structured server-side search (every part must match).
type Criteria struct {
	Text    []string // each word anywhere (headers or text)
	From    []string
	Since   time.Time // only the date counts
	Before  time.Time
	Unseen  bool
	Flagged bool
	Attach  bool // checked on the results (servers cannot search for it)
}

// CriteriaSearcher is implemented by backends that search with Criteria.
type CriteriaSearcher interface {
	SearchCriteria(ctx context.Context, folder string, c Criteria, limit int) ([]Envelope, error)
}

// SearchBy runs a structured search, falling back to a text search whose
// results are filtered here.
func SearchBy(ctx context.Context, b Backend, folder string, c Criteria, limit int) ([]Envelope, error) {
	if s, ok := b.(CriteriaSearcher); ok {
		return s.SearchCriteria(ctx, folder, c, limit)
	}
	envs, err := b.Search(ctx, folder, strings.Join(c.Text, " "), limit)
	if err != nil {
		return nil, err
	}
	out := envs[:0]
	for _, e := range envs {
		if c.keep(e) {
			out = append(out, e)
		}
	}
	return out, nil
}

// keep applies the parts that can be checked on a list row.
func (c Criteria) keep(e Envelope) bool {
	if c.Attach && !e.Attach || c.Unseen && e.Seen || c.Flagged && !e.Flagged {
		return false
	}
	if !c.Since.IsZero() && e.Date.Before(c.Since) || !c.Before.IsZero() && !e.Date.Before(c.Before) {
		return false
	}
	for _, f := range c.From {
		if !strings.Contains(search.Fold(e.From+" "+e.FromAddr), search.Fold(f)) {
			return false
		}
	}
	return true
}

func (c Criteria) imap() *imap.SearchCriteria {
	sc := &imap.SearchCriteria{Text: append([]string(nil), c.Text...), Since: c.Since, Before: c.Before}
	for _, f := range c.From {
		sc.Header = append(sc.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: f})
	}
	if c.Unseen {
		sc.NotFlag = append(sc.NotFlag, imap.FlagSeen)
	}
	if c.Flagged {
		sc.Flag = append(sc.Flag, imap.FlagFlagged)
	}
	return sc
}

// SearchCriteria searches a folder on the server. It uses its own
// connection, so a long search never holds up reading mail, and a search
// that is no longer wanted can be cancelled (that closes only the search
// connection; the next search opens a new one).
func (b *IMAP) SearchCriteria(ctx context.Context, folder string, c Criteria, limit int) ([]Envelope, error) {
	var out []Envelope
	err := b.srch.do(ctx, func(cl *imapclient.Client) error {
		if err := b.srch.ensureSelected(cl, folder); err != nil {
			return err
		}
		sd, err := cl.UIDSearch(c.imap(), &imap.SearchOptions{}).Wait()
		if err != nil {
			return err
		}
		uids := sd.AllUIDs()
		if len(uids) == 0 {
			return nil
		}
		if limit > 0 && len(uids) > limit {
			uids = uids[len(uids)-limit:] // the newest
		}
		msgs, err := cl.Fetch(imap.UIDSetNum(uids...), listFetch).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if e := toEnvelope(m); !c.Attach || e.Attach {
				out = append(out, e)
			}
		}
		return nil
	})
	sortNewest(out)
	return out, err
}

// SearchCriteria searches the in-memory folder (demo mode, tests): headers
// and the decoded text, ignoring case and Turkish accents.
func (m *Memory) SearchCriteria(ctx context.Context, folder string, c Criteria, limit int) ([]Envelope, error) {
	if m.SearchDelay > 0 {
		select {
		case <-time.After(m.SearchDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.mu.Lock()
	type cand struct {
		env Envelope
		raw []byte
	}
	var cands []cand
	for _, x := range m.msgs[folder] {
		if c.keep(x.env) {
			cands = append(cands, cand{x.env, x.raw})
		}
	}
	if m.Calls == nil {
		m.Calls = map[string]int{}
	}
	m.Calls["search"]++
	m.mu.Unlock()
	var out []Envelope
	for _, x := range cands {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hay := search.Fold(x.env.From + " " + x.env.FromAddr + " " + x.env.Subject)
		body := ""
		if len(c.Text) > 0 {
			if p, err := mailmsg.Parse(x.raw); err == nil {
				body = search.Fold(p.Text)
			}
		}
		ok := true
		for _, w := range c.Text {
			f := search.Fold(w)
			if !strings.Contains(hay, f) && !strings.Contains(body, f) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, x.env)
		}
	}
	sortNewest(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
