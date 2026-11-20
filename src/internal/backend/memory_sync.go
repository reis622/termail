package backend

import "context"

// BumpUIDValidity simulates a mailbox rebuild (tests).
func (m *Memory) BumpUIDValidity(folder string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.uidValidity == nil {
		m.uidValidity = map[string]uint32{}
	}
	if m.uidValidity[folder] == 0 {
		m.uidValidity[folder] = 1
	}
	m.uidValidity[folder]++
	m.modSeq[folder]++
}

func (m *Memory) snapshotLocked(folder string) FolderSync {
	if m.uidValidity == nil {
		m.uidValidity = map[string]uint32{}
	}
	if m.uidValidity[folder] == 0 {
		m.uidValidity[folder] = 1
	}
	return FolderSync{
		Folder:        folder,
		UIDValidity:   m.uidValidity[folder],
		UIDNext:       m.nextUID[folder] + 1,
		HighestModSeq: m.modSeq[folder],
	}
}

// LastFolderSync implements IncrementalSyncer.
func (m *Memory) LastFolderSync(folder string) FolderSync {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked(folder)
}

// SyncFolder implements IncrementalSyncer. With DisableCondstore it uses
// the UID SEARCH + flag fallback (no CHANGEDSINCE).
func (m *Memory) SyncFolder(ctx context.Context, state FolderSync, known []uint32) (SyncDelta, error) {
	if err := ctx.Err(); err != nil {
		return SyncDelta{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.snapshotLocked(state.Folder)
	d := SyncDelta{Sync: cur}
	if state.UIDValidity != 0 && state.UIDValidity != cur.UIDValidity {
		d.FullReset = true
		return d, nil
	}
	var current []uint32
	byUID := map[uint32]Envelope{}
	for _, x := range m.msgs[state.Folder] {
		current = append(current, x.env.UID)
		byUID[x.env.UID] = x.env
	}
	added, vanished := uidDiff(known, current)
	d.Vanished = vanished
	for _, u := range added {
		d.New = append(d.New, byUID[u])
	}
	if !m.DisableCondstore && state.HighestModSeq > 0 {
		// "CHANGEDSINCE": anything touched after the bookmark, still present,
		// that is not a brand-new UID (those are in New).
		still := uidSetOf(added)
		for _, x := range m.msgs[state.Folder] {
			if still[x.env.UID] {
				continue
			}
			// Memory has no per-message modseq; after a flag change the
			// folder modseq is higher than the bookmark, so report flags
			// for every surviving known UID when the mailbox moved.
			if cur.HighestModSeq > state.HighestModSeq {
				d.Flags = append(d.Flags, Envelope{UID: x.env.UID, Seen: x.env.Seen, Flagged: x.env.Flagged, Date: x.env.Date, From: x.env.From, FromAddr: x.env.FromAddr, Subject: x.env.Subject, Attach: x.env.Attach, ReadAt: x.env.ReadAt})
			}
		}
	} else if len(known) > 0 {
		// no CONDSTORE: refresh flags of every UID still present
		gone := uidSetOf(vanished)
		for _, u := range known {
			if gone[u] {
				continue
			}
			if e, ok := byUID[u]; ok {
				d.Flags = append(d.Flags, Envelope{UID: e.UID, Seen: e.Seen, Flagged: e.Flagged, Date: e.Date, From: e.From, FromAddr: e.FromAddr, Subject: e.Subject, Attach: e.Attach, ReadAt: e.ReadAt})
			}
		}
	}
	return d, nil
}
