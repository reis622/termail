package ui

import (
	"time"

	"termail/internal/backend"
)

// Local flag changes (saved = \Flagged, read = \Seen) win over server
// snapshots that may be older: a list, Kaydedilenler or new-mail refresh that
// was asked before the change reached the server (or shortly after, since
// some servers, Gmail among them, show a change on another connection a bit
// late) must not undo it. Only a refusal from the server rolls back.

// flagGrace: how long after the server confirmed a change a snapshot may
// still be stale.
const flagGrace = 30 * time.Second

type flagMarkKey struct {
	acct   int
	folder string
	uid    uint32
	msgID  string
	flag   backend.Flag
}

type flagMark struct {
	on        bool
	pending   int       // ops of this mail and flag still on their way
	confirmed time.Time // when the server last accepted it
}

func (m *Model) markKeys(ai int, folder string, e backend.Envelope, f backend.Flag) []flagMarkKey {
	ks := []flagMarkKey{{acct: ai, folder: folder, uid: e.UID, flag: f}}
	id := e.MessageID
	if id == "" && ai < len(m.accts) {
		for _, x := range m.accts[ai].Lists[folder] {
			if x.UID == e.UID {
				id = x.MessageID
				break
			}
		}
	}
	if id != "" {
		ks = append(ks, flagMarkKey{acct: ai, msgID: id, flag: f})
	}
	return ks
}

// noteFlag records an optimistic change that is being sent.
func (m *Model) noteFlag(ai int, folder string, e backend.Envelope, f backend.Flag, on bool) {
	if m.flagMarks == nil {
		m.flagMarks = map[flagMarkKey]*flagMark{}
	}
	for _, k := range m.markKeys(ai, folder, e, f) {
		mk := m.flagMarks[k]
		if mk == nil {
			mk = &flagMark{}
			m.flagMarks[k] = mk
		}
		mk.on = on
		mk.pending++
	}
}

// flagSettled: the server answered one op (err = refused: the mark goes,
// the caller rolls back).
func (m *Model) flagSettled(ai int, folder string, e backend.Envelope, f backend.Flag, err error) {
	for _, k := range m.markKeys(ai, folder, e, f) {
		mk := m.flagMarks[k]
		if mk == nil {
			continue
		}
		if mk.pending > 0 {
			mk.pending--
		}
		if err != nil {
			if mk.pending == 0 {
				delete(m.flagMarks, k)
			}
			continue
		}
		mk.confirmed = time.Now()
	}
}

// localFlag returns the local value that must win over a snapshot asked at
// the given time.
func (m *Model) localFlag(ai int, folder string, e backend.Envelope, f backend.Flag, asked time.Time) (bool, bool) {
	keys := []flagMarkKey{{acct: ai, folder: folder, uid: e.UID, flag: f}}
	if e.MessageID != "" {
		keys = append(keys, flagMarkKey{acct: ai, msgID: e.MessageID, flag: f})
	}
	for _, k := range keys {
		mk := m.flagMarks[k]
		if mk == nil {
			continue
		}
		if mk.pending > 0 || asked.Before(mk.confirmed.Add(flagGrace)) {
			return mk.on, true
		}
		if time.Since(mk.confirmed) > flagGrace {
			delete(m.flagMarks, k) // the server's word is final now
		}
	}
	return false, false
}

// applyMarks puts pending / recently confirmed local flags over a snapshot.
func (m *Model) applyMarks(ai int, folder string, envs []backend.Envelope, asked time.Time) []backend.Envelope {
	if len(m.flagMarks) == 0 {
		return envs
	}
	for i := range envs {
		f := folder
		if envs[i].Folder != "" {
			f = envs[i].Folder
		}
		if on, ok := m.localFlag(ai, f, envs[i], backend.FlagSeen, asked); ok {
			envs[i].Seen = on
		}
		if on, ok := m.localFlag(ai, f, envs[i], backend.FlagFlagged, asked); ok {
			envs[i].Flagged = on
		}
	}
	return envs
}

// mergeSavedSnapshot: the server's Kaydedilenler list with local changes on
// top (a mail saved a moment ago stays, one unsaved a moment ago stays out).
func (m *Model) mergeSavedSnapshot(ai int, envs []backend.Envelope, asked time.Time) []backend.Envelope {
	envs = m.applyMarks(ai, "", envs, asked)
	out := make([]backend.Envelope, 0, len(envs))
	have := func(x backend.Envelope) bool {
		for _, e := range out {
			if e.Folder == x.Folder && e.UID == x.UID || x.MessageID != "" && e.MessageID == x.MessageID {
				return true
			}
		}
		return false
	}
	for _, e := range envs {
		if e.Flagged {
			out = append(out, e)
		}
	}
	if ai < len(m.accts) {
		for _, x := range m.accts[ai].Saved { // saved locally, not in the (older) snapshot yet
			if on, ok := m.localFlag(ai, x.Folder, x, backend.FlagFlagged, asked); ok && on && !have(x) {
				out = append(out, x)
			}
		}
	}
	return out
}
