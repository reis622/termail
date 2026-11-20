package ui

import (
	"termail/internal/backend"
	"termail/internal/grid"

	tea "github.com/charmbracelet/bubbletea"
)

// A small right-click menu. Click outside or Esc closes it.
type ctxMenu struct {
	items []ctxItem
	cur   int
	x, y  int
	w, h  int
}

type ctxItem struct {
	label string
	key   string // the same key the keyboard binding uses, or a special name
	act   func(m *Model) tea.Cmd
}

func (m *Model) openCtxMenu(x, y int, items []ctxItem) {
	if len(items) == 0 {
		return
	}
	w := 18
	for _, it := range items {
		if n := grid.Width(it.label) + 4; n > w {
			w = n
		}
	}
	w = min(w, max(12, m.w-2))
	h := len(items) + 2
	if x+w > m.w {
		x = max(0, m.w-w)
	}
	if y+h > m.h-1 {
		y = max(0, m.h-1-h)
	}
	m.ctxMenu = &ctxMenu{items: items, x: x, y: y, w: w, h: h}
}

func (c *ctxMenu) key(m *Model, key string) tea.Cmd {
	switch key {
	case "esc", "q":
		m.ctxMenu = nil
		return nil
	case "up":
		if c.cur > 0 {
			c.cur--
		}
		return nil
	case "down":
		if c.cur < len(c.items)-1 {
			c.cur++
		}
		return nil
	case "enter", " ":
		return c.choose(m, c.cur)
	}
	return nil
}

func (c *ctxMenu) choose(m *Model, i int) tea.Cmd {
	if i < 0 || i >= len(c.items) {
		m.ctxMenu = nil
		return nil
	}
	act := c.items[i].act
	m.ctxMenu = nil
	if act != nil {
		return act(m)
	}
	return nil
}

func (c *ctxMenu) draw(m *Model, g *grid.Grid) {
	g.Box(c.x, c.y, c.w, c.h, cBorderF, cRaised, "", "")
	m.hits.add(hit{w: g.W, h: g.H, kind: hitModal}) // clicks outside close
	for i, it := range c.items {
		bg := cRaised
		if i == c.cur {
			bg = cSelFocus
		}
		yy := c.y + 1 + i
		g.Fill(c.x+1, yy, c.w-2, 1, bg)
		g.Put(c.x+2, yy, it.label, stbgb(cFG, bg, i == c.cur), c.w-4)
		m.hits.add(hit{x: c.x + 1, y: yy, w: c.w - 2, h: 1, kind: hitCtxItem, idx: i})
	}
}

func (m *Model) mailCtxItems(ai int, fo string, e backend.Envelope) []ctxItem {
	if e.UID == 0 {
		return nil
	}
	save := "Kaydet"
	if e.Flagged {
		save = "Kaldır"
	}
	read := "Okundu"
	if e.Seen {
		read = "Okunmadı"
	}
	run := func(key string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			cmd, _ := m.mailAction(key, ai, fo, e)
			return cmd
		}
	}
	return []ctxItem{
		{"Sekmede aç", "t", run("t")},
		{"Tarayıcıda aç", "o", run("o")},
		{"Yanıtla", "r", run("r")},
		{"Tümüne yanıtla", "a", run("a")},
		{"İlet", "f", run("f")},
		{"Klasöre taşı", "m", run("m")},
		{save, "k", run("k")},
		{read, "i", run("i")},
		{"Sil", "d", run("d")},
	}
}

func (m *Model) showMailMenu(x, y int, ai int, fo string, e backend.Envelope) {
	m.openCtxMenu(x, y, m.mailCtxItems(ai, fo, e))
}

func (m *Model) showFolderMenu(x, y int, r frow) {
	if r.kind == rowAcct {
		ai := r.acct
		m.openCtxMenu(x, y, []ctxItem{{"Yeni klasör", "alt+n", func(m *Model) tea.Cmd {
			m.fcursor = m.folderRowIndex(r)
			m.focus = focusFolders
			return m.startNewFolderFor(ai)
		}}})
		return
	}
	if r.kind != rowFolder || !r.custom {
		return
	}
	m.openCtxMenu(x, y, []ctxItem{
		{"Sil", "d", func(m *Model) tea.Cmd {
			m.fcursor = m.folderRowIndex(r)
			m.focus = focusFolders
			return m.askDeleteFolder(r)
		}},
		{"Yeniden adlandır", "r", func(m *Model) tea.Cmd {
			m.fcursor = m.folderRowIndex(r)
			m.focus = focusFolders
			return m.startRenameFolder(r)
		}},
	})
}

func (m *Model) folderRowIndex(want frow) int {
	for i, r := range m.folderRowsSelectable() {
		if r.kind == want.kind && r.acct == want.acct && r.folder == want.folder && r.read == want.read {
			return i
		}
	}
	return m.fcursor
}
