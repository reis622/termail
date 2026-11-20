package backend

import (
	"context"
	"sort"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// listWindow fetches one newest-first page on the dedicated list session.
// mode is 0=all, 1=unseen, 2=seen. Offset 0 is the newest-by-date window.
func (b *IMAP) listWindow(ctx context.Context, folder string, mode, offset, limit int) ([]Envelope, int, error) {
	var out []Envelope
	var total int
	err := b.doList(ctx, func(s *session, c *imapclient.Client) error {
		if err := s.ensureSelected(c, folder); err != nil {
			return err
		}
		uids, err := b.newestUIDs(c, folder, mode, offset == 0)
		if err != nil {
			return err
		}
		total = len(uids)
		if total == 0 || offset >= total || limit == 0 {
			return nil
		}
		end := offset + limit
		if limit <= 0 || end > total {
			end = total
		}
		msgs, err := c.Fetch(uidSet(uids[offset:end]), listFetch).Collect()
		if err != nil {
			return err
		}
		out = nil
		for _, m := range msgs {
			out = append(out, toEnvelope(m))
		}
		return nil
	})
	sortNewest(out)
	return out, total, err
}

func flagCriteria(mode int) *imap.SearchCriteria {
	sc := &imap.SearchCriteria{}
	switch mode {
	case 1:
		sc.NotFlag = []imap.Flag{imap.FlagSeen}
	case 2:
		sc.Flag = []imap.Flag{imap.FlagSeen}
	}
	return sc
}

// newestUIDs returns all matching UIDs newest-first (by date). A cached
// list is reused for later pages of the same mailbox; offset 0 rebuilds it.
func (b *IMAP) newestUIDs(c *imapclient.Client, folder string, mode int, rebuild bool) ([]uint32, error) {
	if !rebuild {
		b.cacheMu.Lock()
		if b.uidCache.folder == folder && b.uidCache.mode == mode && b.uidCache.uids != nil {
			uids := append([]uint32(nil), b.uidCache.uids...)
			b.cacheMu.Unlock()
			return uids, nil
		}
		b.cacheMu.Unlock()
	}
	uids, err := sortUIDsByDate(c, flagCriteria(mode))
	if err != nil {
		b.cacheMu.Lock()
		b.uidCache = uidPageCache{}
		b.cacheMu.Unlock()
		return nil, err
	}
	b.cacheMu.Lock()
	b.uidCache = uidPageCache{folder: folder, mode: mode, uids: uids}
	b.cacheMu.Unlock()
	return uids, nil
}

func sortUIDsByDate(c *imapclient.Client, sc *imap.SearchCriteria) ([]uint32, error) {
	if c.Caps().Has(imap.CapSort) {
		nums, err := c.UIDSort(&imapclient.SortOptions{
			SearchCriteria: sc,
			SortCriteria:   []imapclient.SortCriterion{{Key: imapclient.SortKeyDate, Reverse: true}},
		}).Wait()
		if err == nil {
			return nums, nil
		}
		// fall through to SEARCH + INTERNALDATE
	}
	sd, err := c.UIDSearch(sc, &imap.SearchOptions{}).Wait()
	if err != nil {
		return nil, err
	}
	raw := sd.AllUIDs()
	if len(raw) == 0 {
		return nil, nil
	}
	ids := make([]uint32, len(raw))
	for i, u := range raw {
		ids[i] = uint32(u)
	}
	msgs, err := c.Fetch(uidSet(ids), &imap.FetchOptions{UID: true, InternalDate: true}).Collect()
	if err != nil {
		return nil, err
	}
	type row struct {
		uid uint32
		at  time.Time
	}
	rows := make([]row, 0, len(msgs))
	for _, m := range msgs {
		rows = append(rows, row{uid: uint32(m.UID), at: m.InternalDate})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].at.Equal(rows[j].at) {
			return rows[i].uid > rows[j].uid
		}
		return rows[i].at.After(rows[j].at)
	})
	out := make([]uint32, len(rows))
	for i, r := range rows {
		out[i] = r.uid
	}
	return out, nil
}
