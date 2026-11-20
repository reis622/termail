package ui

import (
	"maps"
	"slices"

	"termail/internal/i18n"
	"termail/internal/localstore"
)

// View settings remembered between runs (see localstore.Prefs): recent
// searches, grouping by sender, which accounts are expanded in the folder
// panel and when each sender group was last opened. Saving happens in the
// background; the file is tiny.

func (m *Model) loadPrefs() {
	m.prefs = m.opt.Store.LoadPrefs() // (nil store: empty settings)
	m.grouped = m.prefs.Grouped
	if m.prefs.GroupSeen == nil {
		m.prefs.GroupSeen = map[string]int64{}
	}
	if m.opt.Demo && m.opt.Store == nil && len(m.prefs.Recent) == 0 { // sample searches for the demo
		if i18n.Get() == i18n.EN {
			m.prefs.Recent = []string{"invoice", "from:ayşe", "meeting notes", "has:attachment this week", "unread"}
		} else {
			m.prefs.Recent = []string{"fatura", "kimden:ayşe", "toplantı notları", "ekli bu hafta", i18n.T("okunmamış")}
		}
	}
	if len(m.prefs.Open) == 0 {
		return // first run: only the first account is expanded (see New)
	}
	for _, a := range m.accts {
		if open, ok := m.prefs.Open[a.Cfg.Email]; ok {
			a.Open = open
		}
	}
}

// savePrefs writes the current settings in the background.
func (m *Model) savePrefs() {
	m.prefs.Grouped = m.grouped
	open := map[string]bool{}
	for _, a := range m.accts {
		open[a.Cfg.Email] = a.Open
	}
	m.prefs.Open = open
	st := m.opt.Store
	if st == nil {
		return
	}
	p := m.prefs
	p.Recent = slices.Clone(p.Recent)
	p.Open = maps.Clone(p.Open)
	p.GroupSeen = maps.Clone(p.GroupSeen)
	m.bg.Add(1)
	go func() {
		defer m.bg.Done()
		m.prefsMu.Lock() // newest wins: writes never interleave
		defer m.prefsMu.Unlock()
		_ = st.SavePrefs(p)
	}()
}

// setOpen expands / collapses an account in the folder panel (remembered).
func (m *Model) setOpen(ai int, open bool) {
	if ai < 0 || ai >= len(m.accts) || m.accts[ai].Open == open {
		return
	}
	m.accts[ai].Open = open
	m.savePrefs()
}

// addRecent remembers a search (only the typed text).
func (m *Model) addRecent(q string) {
	r := localstore.AddRecent(m.prefs.Recent, q)
	if slices.Equal(r, m.prefs.Recent) {
		return
	}
	m.prefs.Recent = r
	m.savePrefs()
}
