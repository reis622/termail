package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/edit"
	"termail/internal/grid"
	"termail/internal/mailmsg"
)

// ---- attachment picker (Ctrl+E) ----

type attachPicker struct {
	in        edit.Editor
	prefill   string
	pristine  bool
	entries   []string // file/dir paths (dirs end in a separator)
	labels    []string // optional display label (search: "name  ·  dir")
	quick     []quickEntry
	cur       int
	msg       string
	searching bool
	idx       *fileIndex
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func cleanPath(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`)
	if strings.HasPrefix(v, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			v = home + v[1:]
		}
	}
	return v
}

var testFileIndex *fileIndex

func getFileIndex() *fileIndex {
	if testFileIndex != nil {
		return testFileIndex
	}
	if sharedFileIndex == nil {
		sharedFileIndex = newFileIndex(defaultIndexRoots(), defaultIndexCachePath())
	}
	return sharedFileIndex
}

var sharedFileIndex *fileIndex

func newAttachPicker() (*attachPicker, tea.Cmd) {
	in := edit.New(edit.Single)
	in.Placeholder = "yol yapıştır veya dosya adı yaz…"
	start := ""
	if home, err := os.UserHomeDir(); err == nil {
		start = home + string(os.PathSeparator)
		if d := filepath.Join(home, "Documents"); dirExists(d) {
			start = d + string(os.PathSeparator)
		} else if d := filepath.Join(home, "Belgeler"); dirExists(d) {
			start = d + string(os.PathSeparator)
		}
	}
	in.SetValue(start)
	in.CursorEnd()
	idx := getFileIndex()
	p := &attachPicker{in: in, prefill: start, pristine: true, quick: userQuickEntries(), idx: idx}
	p.refresh()
	return p, idx.start()
}

func (p *attachPicker) refresh() {
	v := cleanPath(p.in.Value())
	p.entries, p.labels, p.cur, p.searching = nil, nil, 0, false
	q := strings.TrimSpace(p.in.Value())
	if !looksLikePath(q) && len([]rune(strings.TrimSpace(q))) >= 2 && p.idx != nil {
		p.searching = true
		hits := p.idx.search(strings.TrimSpace(q), 80)
		for _, h := range hits {
			p.entries = append(p.entries, h.Path)
			p.labels = append(p.labels, h.Name+"  ·  "+h.Dir)
		}
		return
	}
	dir, base := filepath.Dir(v), filepath.Base(v)
	if strings.HasSuffix(v, string(os.PathSeparator)) || strings.HasSuffix(v, "/") {
		dir, base = v, ""
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var dirs, files []string
	lb := strings.ToLower(base)
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if lb != "" && !strings.HasPrefix(strings.ToLower(n), lb) {
			continue
		}
		full := filepath.Join(dir, n)
		if e.IsDir() {
			dirs = append(dirs, full+string(os.PathSeparator))
		} else {
			files = append(files, full)
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)
	p.entries = append(dirs, files...)
	if len(p.entries) > 300 {
		p.entries = p.entries[:300]
	}
}

func (p *attachPicker) key(m *Model, c *composer, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		c.picker = nil
		return c.applyFocus()
	case "up":
		p.cur = max(0, p.cur-1)
		return nil
	case "down":
		p.cur = min(len(p.entries)-1, p.cur+1)
		return nil
	case "tab":
		if p.cur < len(p.entries) {
			p.in.SetValue(p.entries[p.cur])
			p.in.CursorEnd()
			p.pristine = false
			p.refresh()
		}
		return nil
	case "enter":
		v := cleanPath(p.in.Value())
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return p.add(c, v)
		}
		if st, err := os.Stat(v); err == nil && st.IsDir() {
			p.in.SetValue(v + string(os.PathSeparator))
			p.in.CursorEnd()
			p.pristine = false
			p.refresh()
			return nil
		}
		if p.cur < len(p.entries) {
			e := p.entries[p.cur]
			if strings.HasSuffix(e, string(os.PathSeparator)) || dirExists(e) {
				if !strings.HasSuffix(e, string(os.PathSeparator)) {
					e += string(os.PathSeparator)
				}
				p.in.SetValue(e)
				p.in.CursorEnd()
				p.pristine = false
				p.refresh()
				return nil
			}
			return p.add(c, e)
		}
		p.msg = "Dosya bulunamadı."
		return nil
	}
	if p.pristine && k.Type == tea.KeyRunes && len(k.Runes) > 0 {
		r := k.Runes[0]
		if k.Paste || strings.ContainsRune(`/\"'~`, r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			p.in.SetValue("")
		}
	}
	_, cmd := m.editKey(&p.in, k)
	if p.pristine {
		v := p.in.Value()
		if rest, ok := strings.CutPrefix(v, p.prefill); ok && len(rest) == 2 && rest[1] == ':' {
			p.in.SetValue(rest)
			p.in.CursorEnd()
		}
		if v != p.prefill {
			p.pristine = false
		}
	}
	p.msg = ""
	p.refresh()
	return cmd
}

const maxAttach = 25 << 20

func (p *attachPicker) add(c *composer, path string) tea.Cmd {
	st, err := os.Stat(path)
	if err != nil {
		p.msg = "Dosya açılamadı."
		return nil
	}
	if st.Size() > maxAttach {
		p.msg = "Dosya çok büyük (en fazla 25 MB)."
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		p.msg = "Dosya okunamadı."
		return nil
	}
	c.atts = append(c.atts, mailmsg.Attachment{Name: filepath.Base(path), ContentType: mailmsg.TypeByName(path), Size: len(data), Data: data})
	c.picker = nil
	if mailmsg.Risky(filepath.Base(path), data) {
		c.status, c.statusFG = filepath.Base(path)+" bir program dosyası; Gmail ve Outlook bu tür ekleri geri çevirir, ileti ulaşmaz.", cYellow
	}
	return c.applyFocus()
}

func (p *attachPicker) goDir(path string) {
	if !dirExists(path) {
		p.msg = "Klasör açılamadı."
		return
	}
	p.in.SetValue(path + string(os.PathSeparator))
	p.in.CursorEnd()
	p.pristine = false
	p.msg = ""
	p.refresh()
}

func (p *attachPicker) draw(g *grid.Grid) {
	w := min(96, g.W-6)
	h := min(22, g.H-6)
	x, y := (g.W-w)/2, (g.H-h)/2
	g.Box(x, y, w, h, cBorderF, cPanel, "Ek ekle", cAccent)
	g.Put(x+3, y+1, "Yol:", st(cDim), 0)
	g.Fill(x+8, y+1, w-11, 1, cBG)
	p.in.Draw(g, x+9, y+1, w-13, 1, editStyle(cBG))

	// quick access row
	qx, qy := x+3, y+2
	g.Put(qx, qy, "Hızlı:", st(cFaint), 0)
	qx += 7
	for i, e := range p.quick {
		lab := e.label
		tw := grid.Width(lab) + 2
		if qx+tw >= x+w-2 {
			break
		}
		bg := cPanel
		g.Put(qx, qy, lab, stbg(cAccent, bg), 0)
		// hits added by caller via collectHits
		p.quick[i].path = e.path // keep
		qx += tw + 1
		_ = i
	}

	rows := h - 7
	listTop := y + 4
	if p.searching {
		g.Put(x+3, y+3, "Arama sonuçları", st(cDim), w-6)
	} else {
		g.Put(x+3, y+3, "Dosyayı pencereye sürükleyip bırakabilir ya da yolunu yazabilirsin.", st(cFaint), w-6)
	}
	start := 0
	if p.cur >= rows {
		start = p.cur - rows + 1
	}
	for i := start; i < len(p.entries) && i-start < rows; i++ {
		ry := listTop + i - start
		bg := cPanel
		if i == p.cur {
			bg = cSelFocus
		}
		g.Fill(x+1, ry, w-2, 1, bg)
		name := filepath.Base(strings.TrimRight(p.entries[i], `/\`))
		icon, fg := "▪ ", cFG
		if strings.HasSuffix(p.entries[i], string(os.PathSeparator)) {
			icon, fg, name = "▸ ", cAccent, name+string(os.PathSeparator)
		}
		if i < len(p.labels) && p.labels[i] != "" {
			name = p.labels[i]
		}
		g.Put(x+3, ry, icon+name, stbgb(fg, bg, i == p.cur), w-6)
	}
	if p.msg != "" {
		g.Put(x+3, y+h-2, p.msg, st(cRed), w-6)
	} else if p.searching && len(p.entries) == 0 {
		hint := "Dizin taranıyor…"
		if p.idx != nil && p.idx.hasData() {
			hint = "Eşleşen dosya yok."
		}
		g.Put(x+3, y+h-2, hint, st(cFaint), w-6)
	}
	keybar(g, g.H-1, []keyHint{{"Enter", "Ekle / klasöre gir"}, {"Tab", "Tamamla"}, {"↑↓", "Seç"}, {"Esc", "Vazgeç"}}, "", nil)
}

func (p *attachPicker) addHits(m *Model, g *grid.Grid) {
	w := min(96, g.W-6)
	h := min(22, g.H-6)
	x, y := (g.W-w)/2, (g.H-h)/2
	m.hits.add(hit{x: x + 8, y: y + 1, w: w - 11, h: 1, kind: hitPickerPath, ox: x + 9, oy: y + 1})
	qx, qy := x+3+7, y+2
	for _, e := range p.quick {
		lab := e.label
		tw := grid.Width(lab) + 2
		if qx+tw >= x+w-2 {
			break
		}
		m.hits.add(hit{x: qx, y: qy, w: tw, h: 1, kind: hitPickerQuick, s: e.path})
		qx += tw + 1
	}
	rows := h - 7
	listTop := y + 4
	start := 0
	if p.cur >= rows {
		start = p.cur - rows + 1
	}
	for i := start; i < len(p.entries) && i-start < rows; i++ {
		ry := listTop + i - start
		m.hits.add(hit{x: x + 1, y: ry, w: w - 2, h: 1, kind: hitPickerItem, idx: i})
	}
}
