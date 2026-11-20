package ui

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif" // decoders for the image preview
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/downloads"
	"termail/internal/grid"
	"termail/internal/htmltext"
	"termail/internal/i18n"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

// The reading pane has two tabs: "Mail" and "Ekler ve Bağlantılar (A · L)".
// e toggles them (Shift+E opens a mail straight on the second one); the
// second lists the attachments (type chip, name, size), a quick preview of
// the selected attachment, then the links (their text, the real domain
// beneath, a warning when the text names another address). r.attMode is
// "the second tab is shown".

// tabItem is one row that can be selected on the second tab.
type tabItem struct {
	att  int    // attachment index, or -1
	link int    // link index, or -1
	code string // verification / OTP code, if set
}

func tabItems(p *mailmsg.Parsed) []tabItem {
	if p == nil {
		return nil
	}
	var out []tabItem
	for _, c := range verifyCodes(p) {
		out = append(out, tabItem{att: -1, link: -1, code: c})
	}
	for i := range p.Attachments {
		out = append(out, tabItem{att: i, link: -1})
	}
	for i := range p.Links {
		out = append(out, tabItem{att: -1, link: i})
	}
	return out
}

// tabsTitle is "Ekler ve Bağlantılar (3 · 5)" or, when a verification
// code is present, "Ekler ve Bağlantılar (3 · 5 · 1 kod)".
func tabsTitle(p *mailmsg.Parsed) string {
	a, l, c := 0, 0, 0
	if p != nil {
		a, l, c = len(p.Attachments), len(p.Links), len(verifyCodes(p))
	}
	if c > 0 {
		return i18n.Tf("Ekler ve Bağlantılar (%d · %d · %d kod)", a, l, c)
	}
	return i18n.Tf("Ekler ve Bağlantılar (%d · %d)", a, l)
}

// attChip names the kind of an attachment and its chip colour.
func attChip(a mailmsg.Attachment) (string, string) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(a.Name), "."))
	ct := strings.ToLower(a.ContentType)
	switch {
	case ext == "pdf" || ct == "application/pdf":
		return "PDF", cRed
	case ext == "csv" || ext == "xls" || ext == "xlsx" || ext == "ods" || strings.Contains(ct, "spreadsheet") || ct == "text/csv":
		return "Tablo", cGreen
	case strings.HasPrefix(ct, "image/") || ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "gif" || ext == "bmp" || ext == "webp":
		return "Resim", cYellow
	case ext == "zip" || ext == "rar" || ext == "7z" || ext == "tar" || ext == "gz" || strings.Contains(ct, "zip"):
		return i18n.T("Arşiv"), cPurple
	case strings.HasPrefix(ct, "text/") || ext == "txt" || ext == "md" || ext == "log" || ext == "json" || ext == "xml" || ext == "ics":
		return "Metin", cAccent
	}
	return "Dosya", cDim
}

// riskyExt: files that run as programs when opened; they need an "e".
var riskyExt = map[string]bool{"exe": true, "com": true, "bat": true, "cmd": true, "scr": true, "pif": true, "msi": true, "msp": true,
	"ps1": true, "psm1": true, "vbs": true, "vbe": true, "js": true, "jse": true, "wsf": true, "wsh": true, "hta": true, "jar": true,
	"lnk": true, "reg": true, "cpl": true, "msc": true, "dll": true, "inf": true, "scf": true, "url": true, "appx": true, "msix": true,
	"application": true, "gadget": true, "iso": true, "img": true, "vhd": true, "vhdx": true, "chm": true, "sct": true, "xll": true}

func riskyAttachment(name string) bool {
	return riskyExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(strings.TrimRight(name, ". ")), "."))]
}

// linkLabel is what a link row says: its anchor text, or (for a bare
// address in the text) the sentence right before it in the mail.
func linkLabel(p *mailmsg.Parsed, i int) string {
	l := p.Links[i]
	t := strings.TrimSpace(l.Text)
	if t != "" && !strings.Contains(t, "://") && !strings.HasPrefix(strings.ToLower(t), "mailto:") && t != htmltext.Domain(l.URL) {
		return t
	}
	if s := sentenceBefore(viewText(p), i+1); s != "" {
		return s
	}
	if t != "" {
		return t
	}
	return htmltext.Shorten(l.URL, 60)
}

// sentenceBefore finds the text before link marker n ("\uE000[n]"), back to
// the previous sentence end or line break.
func sentenceBefore(text string, n int) string {
	mk := string(htmltext.MarkRune) + "[" + strconv.Itoa(n) + "]"
	j := strings.Index(text, mk)
	if j < 0 {
		return ""
	}
	before := text[:j]
	// drop the bare address itself when it is printed right before its marker
	before = strings.TrimRight(before, " ")
	if k := strings.LastIndexAny(before, " \n"); k >= 0 && strings.Contains(before[k:], "://") {
		before = before[:k]
	}
	start := 0
	for k := len(before) - 1; k >= 0; k-- {
		c := before[k]
		if c == '\n' || (c == '.' || c == '!' || c == '?') && k < len(before)-1 && before[k+1] == ' ' {
			start = k + 1
			break
		}
	}
	s := strings.Join(strings.Fields(before[start:]), " ")
	s = strings.TrimRight(s, ":;,-–— ")
	// markers of earlier links inside the sentence
	for strings.ContainsRune(s, htmltext.MarkRune) {
		a := strings.IndexRune(s, htmltext.MarkRune)
		b := strings.IndexByte(s[a:], ']')
		if b < 0 {
			break
		}
		s = s[:a] + s[a+b+1:]
	}
	if utf8.RuneCountInString(s) > 70 {
		r := []rune(s)
		s = "…" + string(r[len(r)-69:])
	}
	return s
}

// ---- keys ----

// showAttTab opens the second tab (sel = first row).
func (r *reader) showAttTab(m *Model) {
	p := m.body(r.key)
	if p != nil && len(p.Attachments) == 0 && len(p.Links) == 0 && len(verifyCodes(p)) == 0 {
		m.toastInfo(toast.Info, "", i18n.T("Bu iletide ek ya da bağlantı yok."))
		return
	}
	r.attMode, r.sel, r.tabTop, r.showAtt = true, 0, 0, false
	r.att = 0
}

// attModeKey handles a key on the second tab. handled=false: the key does
// what it does in the mail (r, d, k …); the tab stays.
func (r *reader) attModeKey(m *Model, key string) (tea.Cmd, bool) {
	p := m.body(r.key)
	if p == nil {
		switch key {
		case "e", "esc":
			r.attMode = false
			return nil, true
		}
		return nil, false
	}
	items := tabItems(p)
	n := len(items)
	pick := func(i int) {
		r.sel = i
		if it := items[i]; it.att >= 0 {
			r.att = it.att
		}
	}
	switch key {
	case "e", "esc", "left":
		r.attMode = false
	case "up", "k":
		if n > 0 {
			pick(max(0, r.sel-1))
		}
	case "down", "j":
		if n > 0 {
			pick(min(n-1, r.sel+1))
		}
	case "home":
		if n > 0 {
			pick(0)
		}
	case "end":
		if n > 0 {
			pick(n - 1)
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0":
		// the links carry the numbers [1]…; with no links, attachments do
		d := int(key[0] - '1')
		if key == "0" {
			d = 9
		}
		switch {
		case len(p.Links) > 0 && d < len(p.Links):
			pick(len(p.Attachments) + d)
		case len(p.Links) == 0 && d < len(p.Attachments):
			pick(d)
		default:
			m.toastInfo(toast.Info, "", i18n.T("Bu numarada bağlantı yok."))
		}
	case "enter", "o", "v":
		if n == 0 {
			return nil, true
		}
		return r.openItem(m, p, items[min(r.sel, n-1)]), true
	case "s":
		if n > 0 && items[min(r.sel, n-1)].att >= 0 {
			return r.saveAttachment(m), true
		}
		m.toastInfo(toast.Info, "", i18n.T("Bağlantılar kaydedilmez; y ile adresini kopyalayabilirsin."))
	case "y":
		if n > 0 {
			if it := items[min(r.sel, n-1)]; it.link >= 0 {
				copyTextFn(p.Links[it.link].URL)
				m.toastInfo(toast.Success, "", i18n.T("Kopyalandı"))
			}
		}
	default:
		return nil, false
	}
	return nil, true
}

// wheelTab: the wheel moves the selection on the second tab one row per
// notch (the list scrolls with it).
func (r *reader) wheelTab(m *Model, d int) tea.Cmd {
	k := "down"
	if d < 0 {
		k = "up"
	}
	r.attModeKey(m, k)
	return nil
}

// selectedKey is the body key of the selected list mail.
func (m *Model) selectedKey() bodyKey {
	e, ok := m.selected()
	if !ok {
		return bodyKey{}
	}
	ai, fo := m.loc(e)
	return bodyKey{ai, fo, e.UID}
}

func (r *reader) openItem(m *Model, p *mailmsg.Parsed, it tabItem) tea.Cmd {
	if it.code != "" {
		copyTextFn(it.code)
		m.toastInfo(toast.Success, "", i18n.T("Kod kopyalandı: ")+it.code)
		return nil
	}
	if it.link >= 0 {
		return m.openLink(p.Links[it.link].URL)
	}
	return m.openAttachment(p.Attachments[it.att])
}

// ---- opening an attachment with its Windows program ----

// Hooks (replaced in tests): where attachments are put to be opened, and
// how a file is handed to its default program (ShellExecute on Windows; the
// path is one argument, never through a shell).
var (
	attTempDir = func() string { return filepath.Join(os.TempDir(), "Termail") }
	openFileFn = openFile
)

type attOpenMsg struct {
	path string
	err  error
}

func (m *Model) openAttachment(a mailmsg.Attachment) tea.Cmd {
	if riskyAttachment(a.Name) {
		name := grid.Clean(a.Name)
		m.confirm = &confirmBox{strict: true, title: i18n.T("Program dosyası"),
			text: "“" + name + "” bir program dosyası; açılırsa bilgisayarında çalışır. Yalnızca gönderene güveniyorsan aç.\n\nAçılsın mı? (e/h)",
			yes:  i18n.T("Evet, aç"), onYes: func() tea.Cmd { return m.launchAttachment(a) }}
		return nil
	}
	return m.launchAttachment(a)
}

func (m *Model) launchAttachment(a mailmsg.Attachment) tea.Cmd {
	m.toastInfo(toast.Info, "", i18n.T("Açılıyor…"))
	return func() tea.Msg {
		dir := attTempDir()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return attOpenMsg{err: err}
		}
		path, err := downloads.Save(dir, a.Name, a.Data)
		if err == nil {
			err = openFileFn(path)
		}
		return attOpenMsg{path: path, err: err}
	}
}

func (m *Model) onAttOpen(msg attOpenMsg) {
	if msg.err != nil {
		m.logErr("open attachment", msg.err)
		m.toasts.Push(toast.Error, "", i18n.T("Ek açılamadı; s ile İndirilenler'e kaydedip oradan açabilirsin."), m.opt.Now(), 6*time.Second)
	}
}

// ---- drawing ----

// drawReaderTabs draws "Mail  Ekler ve Bağlantılar (3 · 5)" and returns
// the row under it.
func (m *Model) drawReaderTabs(g *grid.Grid, x, y, w int, r *reader, p *mailmsg.Parsed) {
	labels := []string{i18n.T("Mail"), tabsTitle(p)}
	cx := x
	for i, l := range labels {
		on := i == 0 && !r.attMode || i == 1 && r.attMode
		if on {
			g.Put(cx, y, "▌", st(cAccent), 0)
			end := g.Put(cx+1, y, l+" ", stbgb(cFG, cSelFocus, true), max(1, x+w-cx-1))
			m.hits.add(hit{x: cx, y: y, w: end - cx, h: 1, kind: hitReaderTab, idx: i})
			cx = end + 1
		} else {
			end := g.Put(cx+1, y, l, st(cDim), max(1, x+w-cx-1))
			m.hits.add(hit{x: cx, y: y, w: end - cx, h: 1, kind: hitReaderTab, idx: i})
			cx = end + 2
		}
		if cx >= x+w {
			break
		}
	}
}

// tabRow is one drawn row of the second tab.
type tabRow struct {
	item int // selectable item index, or -1
	draw func(y int, sel bool)
}

// drawAttTab draws the second tab between y and bottom (exclusive).
func (m *Model) drawAttTab(g *grid.Grid, x, y, w, bottom int, r *reader, p *mailmsg.Parsed, focused bool) {
	if p == nil {
		g.Put(x, y, i18n.T("Yükleniyor…"), st(cFaint), w)
		return
	}
	items := tabItems(p)
	if len(items) == 0 {
		g.Put(x, y, i18n.T("Bu iletide ek ya da bağlantı yok."), st(cFaint), w)
		return
	}
	r.sel = min(r.sel, len(items)-1)
	var rows []tabRow
	blank := tabRow{item: -1, draw: func(int, bool) {}}
	heading := func(s string) tabRow {
		return tabRow{item: -1, draw: func(yy int, _ bool) { g.Put(x, yy, s, stb(cAccent), w) }}
	}
	selFill := func(yy int, sel bool) string {
		if !sel {
			return ""
		}
		bg := cSel
		if focused {
			bg = cSelFocus
		}
		g.Fill(x, yy, w, 1, bg)
		g.Put(x-1, yy, "▌", st(cAccent), 0)
		return bg
	}
	codes := verifyCodes(p)
	if len(codes) > 0 {
		rows = append(rows, heading(i18n.T("Doğrulama kodu")))
		for i, c := range codes {
			i, c := i, c
			rows = append(rows, tabRow{item: i, draw: func(yy int, sel bool) {
				bg := selFill(yy, sel)
				cx := g.Put(x, yy, " Kod ", grid.Style{FG: cBG, BG: cGreen, Bold: true}, 0)
				cx = g.Put(cx+1, yy, c, grid.Style{FG: cFG, BG: bg, Bold: true}, 0)
				g.Put(cx+2, yy, "Enter: kopyala", grid.Style{FG: cDim, BG: bg}, max(1, x+w-cx-2))
			}})
		}
		rows = append(rows, blank)
	}
	attBase := len(codes)
	if len(p.Attachments) > 0 {
		rows = append(rows, heading(i18n.T("Ekler")))
		for i, a := range p.Attachments {
			i, a := i, a
			rows = append(rows, tabRow{item: attBase + i, draw: func(yy int, sel bool) {
				bg := selFill(yy, sel)
				chip, col := attChip(a)
				cx := g.Put(x, yy, " "+chip+" ", grid.Style{FG: cBG, BG: col, Bold: true}, 0)
				size := mailmsg.HumanSize(a.Size)
				g.Put(cx+1, yy, a.Name, grid.Style{FG: cFG, BG: bg, Bold: sel}, max(1, x+w-cx-3-grid.Width(size)))
				g.RPut(x+w-1, yy, size, grid.Style{FG: cDim, BG: bg})
			}})
		}
		if it := items[r.sel]; it.att >= 0 {
			a := p.Attachments[it.att]
			rows = append(rows, blank, tabRow{item: -1, draw: func(yy int, _ bool) {
				cx := g.Put(x, yy, i18n.T("Önizleme"), stb(cAccent), 0)
				g.Put(cx, yy, "  ·  "+a.Name, stb(cAccent), max(1, x+w-cx))
			}})
			for _, pl := range r.previewLines(it.att, a, w, max(4, min(14, (bottom-y)/2))) {
				pl := pl
				rows = append(rows, tabRow{item: -1, draw: func(yy int, _ bool) { pl.draw(g, x, yy, w) }})
			}
		}
		rows = append(rows, blank)
	}
	if len(p.Links) > 0 {
		rows = append(rows, heading(i18n.T("Bağlantılar")))
		nw := len(strconv.Itoa(len(p.Links))) + 2
		for i, l := range p.Links {
			i, l := i, l
			item := attBase + len(p.Attachments) + i
			warn := htmltext.Mismatch(l.Text, l.URL)
			fg := cFG
			if warn {
				fg = cRed
			}
			rows = append(rows, tabRow{item: item, draw: func(yy int, sel bool) {
				bg := selFill(yy, sel)
				mf := cAccent
				if warn {
					mf = cRed
				}
				cx := g.Put(x, yy, fmt.Sprintf("%-*s", nw, "["+strconv.Itoa(i+1)+"]"), grid.Style{FG: mf, BG: bg, Bold: true}, 0)
				g.Put(cx+1, yy, linkLabel(p, i), grid.Style{FG: fg, BG: bg, Bold: sel}, max(1, x+w-cx-1))
			}}, tabRow{item: item, draw: func(yy int, _ bool) {
				dfg := cDim
				if warn {
					dfg = cRed
				}
				g.Put(x+nw+1, yy, htmltext.Domain(l.URL), st(dfg), max(1, w-nw-1))
			}})
			if warn {
				rows = append(rows, tabRow{item: item, draw: func(yy int, _ bool) {
					g.Put(x+nw+1, yy, i18n.T("Dikkat: farklı adrese gidiyor"), stb(cRed), max(1, w-nw-1))
				}})
			}
		}
	}
	// scroll so the selected item's rows are on screen
	h := bottom - y
	first, last := -1, -1
	for i, rw := range rows {
		if rw.item == r.sel {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first >= 0 {
		if first < r.tabTop {
			r.tabTop = first
		}
		if last >= r.tabTop+h {
			r.tabTop = last - h + 1
		}
	}
	r.tabTop = max(0, min(r.tabTop, len(rows)-h))
	for i := r.tabTop; i < len(rows) && y < bottom; i++ {
		rw := rows[i]
		rw.draw(y, rw.item == r.sel && rw.item >= 0 && (i == 0 || rows[i-1].item != rw.item))
		if rw.item >= 0 {
			m.hits.add(hit{x: x, y: y, w: w, h: 1, kind: hitTabItem, idx: rw.item})
		}
		y++
	}
}

// ---- quick preview ----

type prevLine struct {
	text string
	fg   string
	bold bool
	px   [][2]string // half-block image cells: top, bottom colour
}

func (pl prevLine) draw(g *grid.Grid, x, y, w int) {
	if pl.px != nil {
		for i, c := range pl.px {
			if i >= w {
				break
			}
			g.Put(x+i, y, "▀", grid.Style{FG: c[0], BG: c[1]}, 0)
		}
		return
	}
	fg := pl.fg
	if fg == "" {
		fg = cFG
	}
	g.Put(x, y, pl.text, grid.Style{FG: fg, Bold: pl.bold}, w)
}

func (r *reader) previewLines(i int, a mailmsg.Attachment, w, h int) []prevLine {
	if r.prev != nil && r.prevAtt == i && r.prevW == w && r.prevH == h {
		return r.prev
	}
	r.prev, r.prevAtt, r.prevW, r.prevH = attPreview(a, w, h), i, w, h
	return r.prev
}

// attPreview renders the first lines of an attachment: text, CSV as an
// aligned table, JSON indented, PNG/JPEG/GIF as half-block art.
func attPreview(a mailmsg.Attachment, w, h int) []prevLine {
	chip, _ := attChip(a)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(a.Name), "."))
	note := func(s string) []prevLine { return []prevLine{{text: s, fg: cFaint}} }
	switch {
	case chip == "PDF":
		return note(i18n.T("PDF önizlemesi yok, Enter ile aç."))
	case chip == "Resim":
		if l := imagePreview(a.Data, w, h); l != nil {
			return l
		}
		return note(i18n.T("Bu resim önizlenemiyor, Enter ile aç."))
	}
	if len(a.Data) == 0 || bytes.IndexByte(a.Data, 0) >= 0 || !utf8.Valid(a.Data) {
		return note(i18n.T("Önizleme yok; Enter ile aç, s ile kaydet."))
	}
	text := strings.ReplaceAll(string(a.Data), "\r\n", "\n")
	if ext == "csv" || strings.EqualFold(a.ContentType, "text/csv") {
		if l := csvPreview(text, w, h); l != nil {
			return l
		}
	}
	if ext == "json" || strings.Contains(strings.ToLower(a.ContentType), "json") {
		var buf bytes.Buffer
		if json.Indent(&buf, []byte(text), "", "  ") == nil {
			text = buf.String()
		}
	}
	var out []prevLine
	for _, l := range strings.Split(text, "\n") {
		if len(out) >= h {
			out = append(out, prevLine{text: "…", fg: cFaint})
			break
		}
		out = append(out, prevLine{text: strings.ReplaceAll(l, "\t", "    ")})
	}
	return out
}

func csvPreview(text string, w, h int) []prevLine {
	sep := ','
	if first, _, _ := strings.Cut(text, "\n"); strings.Count(first, ";") > strings.Count(first, ",") {
		sep = ';'
	}
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma, cr.FieldsPerRecord, cr.LazyQuotes = sep, -1, true
	recs, err := cr.ReadAll()
	if err != nil || len(recs) == 0 {
		return nil
	}
	if len(recs) > h {
		recs = recs[:h]
	}
	cols := 0
	for _, rec := range recs {
		cols = max(cols, len(rec))
	}
	width := make([]int, cols)
	numeric := make([]bool, cols)
	for c := range numeric {
		numeric[c] = true
	}
	for ri, rec := range recs {
		for c, v := range rec {
			v = strings.TrimSpace(v)
			width[c] = max(width[c], grid.Width(v))
			if ri > 0 && v != "" && strings.Trim(v, "0123456789.,-+% ₺$€") != "" {
				numeric[c] = false
			}
		}
	}
	row := func(rec []string) string {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			v := ""
			if c < len(rec) {
				v = strings.TrimSpace(rec[c])
			}
			pad := strings.Repeat(" ", max(0, width[c]-grid.Width(v)))
			if c > 0 {
				b.WriteString("   ")
			}
			if numeric[c] && c > 0 {
				b.WriteString(pad + v)
			} else {
				b.WriteString(v + pad)
			}
		}
		return strings.TrimRight(b.String(), " ")
	}
	total := 0
	for c := range width {
		total += width[c] + 3
	}
	out := []prevLine{{text: row(recs[0]), fg: cDim}, {text: strings.Repeat("─", max(1, min(w, total-3))), fg: cBorder}}
	for _, rec := range recs[1:] {
		out = append(out, prevLine{text: row(rec)})
	}
	return out
}

// imagePreview: a small half-block picture (two pixels per cell).
func imagePreview(data []byte, w, h int) []prevLine {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > 40_000_000 {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	cw := min(w, 48)
	ch := cw * b.Dy() / max(1, b.Dx()) / 2 // rows of two pixels
	if ch > h {
		ch = h
		cw = max(1, ch*2*b.Dx()/max(1, b.Dy()))
	}
	ch = max(1, ch)
	px := func(cx, py int) string {
		sx := b.Min.X + cx*b.Dx()/cw
		sy := b.Min.Y + py*b.Dy()/(ch*2)
		r, g, bl, a := img.At(sx, sy).RGBA()
		if a == 0 {
			return cBG
		}
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, bl>>8)
	}
	out := make([]prevLine, 0, ch)
	for y := 0; y < ch; y++ {
		cells := make([][2]string, cw)
		for x := 0; x < cw; x++ {
			cells[x] = [2]string{px(x, 2*y), px(x, 2*y+1)}
		}
		out = append(out, prevLine{px: cells})
	}
	return out
}
