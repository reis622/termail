package backend

import (
	"context"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

var flagFetch = &imap.FetchOptions{UID: true, Flags: true}

// SyncFolder implements IncrementalSyncer. When the server speaks CONDSTORE
// it uses CHANGEDSINCE; otherwise it falls back to UID SEARCH + a FLAGS
// fetch of known messages (and new UIDs). QRESYNC VANISHED is not used
// (this go-imap client has no VANISHED handler); vanished UIDs come from
// a UID SEARCH difference, which also covers Outlook-style servers.
func (b *IMAP) SyncFolder(ctx context.Context, state FolderSync, known []uint32) (SyncDelta, error) {
	folder := state.Folder
	if folder == "" {
		folder = "INBOX"
		state.Folder = folder
	}
	var d SyncDelta
	err := b.list.do(ctx, func(c *imapclient.Client) error {
		sel, err := b.list.selectLocked(c, folder)
		if err != nil {
			return err
		}
		cur := FolderSync{Folder: folder, UIDValidity: sel.UIDValidity, UIDNext: uint32(sel.UIDNext), HighestModSeq: sel.HighestModSeq}
		d.Sync = cur
		if state.UIDValidity != 0 && state.UIDValidity != cur.UIDValidity {
			d.FullReset = true
			return nil
		}
		sd, err := c.UIDSearch(&imap.SearchCriteria{}, &imap.SearchOptions{}).Wait()
		if err != nil {
			return err
		}
		raw := sd.AllUIDs()
		current := make([]uint32, len(raw))
		for i, u := range raw {
			current[i] = uint32(u)
		}
		added, vanished := uidDiff(known, current)
		d.Vanished = vanished

		if hasCondstore(c) && state.HighestModSeq > 0 && sel.NumMessages > 0 {
			var set imap.SeqSet
			set.AddRange(1, sel.NumMessages)
			msgs, err := c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true, ChangedSince: state.HighestModSeq}).Collect()
			if err != nil {
				return err
			}
			isNew := uidSetOf(added)
			for _, msg := range msgs {
				e := toEnvelope(msg)
				if isNew[e.UID] {
					continue // full headers fetched below
				}
				d.Flags = append(d.Flags, Envelope{UID: e.UID, Seen: e.Seen, Flagged: e.Flagged})
			}
		} else if len(known) > 0 {
			rest := make([]uint32, 0, len(known))
			gone := uidSetOf(vanished)
			for _, u := range known {
				if !gone[u] {
					rest = append(rest, u)
				}
			}
			if len(rest) > 0 {
				msgs, err := c.Fetch(uidSet(rest), flagFetch).Collect()
				if err != nil {
					return err
				}
				for _, msg := range msgs {
					e := toEnvelope(msg)
					d.Flags = append(d.Flags, Envelope{UID: e.UID, Seen: e.Seen, Flagged: e.Flagged})
				}
			}
		}
		if len(added) > 0 {
			msgs, err := c.Fetch(uidSet(added), listFetch).Collect()
			if err != nil {
				return err
			}
			for _, msg := range msgs {
				d.New = append(d.New, toEnvelope(msg))
			}
			sortNewest(d.New)
		}
		return nil
	})
	return d, err
}
