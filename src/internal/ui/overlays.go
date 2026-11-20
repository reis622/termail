package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"termail/internal/backend"
	"termail/internal/blocklist"
	"termail/internal/grid"
	"termail/internal/mailmsg"
	"termail/internal/toast"
	"termail/internal/undo"
)

type bodyRef struct{ key bodyKey }

// ---- text helpers ----

// wrapText wraps text at w cells, keeping existing line breaks.
func wrapText(s string, w int) []string {
	lines, _ := wrapSpans(s, w)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return out
}

// wline is one wrapped line and where it came from, so a selection on
// screen can be turned back into the original text: text[pre:] is exactly
// paras[para][start:end] (pre is the repeated "> " of a wrapped quote).
type wline struct {
	text       string
	para       int
	start, end int
	pre        int
}

// wrapSpans wraps like wrapText and also returns the source paragraphs
// (tabs expanded, trailing spaces dropped) the lines point into.
func wrapSpans(s string, w int) ([]wline, []string) {
	if w < 4 {
		w = 4
	}
	paras := strings.Split(strings.ReplaceAll(s, "\t", "    "), "\n")
	var out []wline
	for pi, line := range paras {
		line = strings.TrimRight(line, " \r")
		paras[pi] = line
		if runewidth.StringWidth(line) <= w {
			out = append(out, wline{text: line, para: pi, start: 0, end: len(line)})
			continue
		}
		prefix := ""
		if strings.HasPrefix(line, ">") {
			i := 0
			for i < len(line) && (line[i] == '>' || line[i] == ' ') {
				i++
			}
			prefix = line[:i]
		}
		cur, curStart, curPre := "", 0, 0
		emit := func() {
			p := min(curPre, len(cur)) // a very long quote prefix can fill a whole line
			out = append(out, wline{text: cur, para: pi, start: curStart, end: curStart + len(cur) - p, pre: p})
		}
		pos := 0
		for _, word := range strings.Split(line, " ") {
			ws := pos
			pos += len(word) + 1
			cand := word
			if cur != "" {
				cand = cur + " " + word
			}
			if runewidth.StringWidth(cand) <= w {
				if cur == "" {
					curStart, curPre = ws, 0
				}
				cur = cand
				continue
			}
			if cur != "" {
				emit()
				cur, curStart, curPre = prefix+word, ws, len(prefix)
			} else {
				cur, curStart, curPre = word, ws, 0
			}
			for runewidth.StringWidth(cur) > w { // very long word / link
				cut := runewidth.Truncate(cur, w, "")
				if cut == "" {
					break
				}
				save := cur
				cur = cut
				emit()
				cur = save[len(cut):]
				if len(cut) >= curPre {
					curStart += len(cut) - curPre
					curPre = 0
				} else {
					curPre -= len(cut)
				}
			}
		}
		if cur != "" {
			emit()
		}
	}
	return out, paras
}

// ---- search ----

// ---- confirm dialog ----

type confirmBox struct {
	title, text, yes string
	onYes            func() tea.Cmd
	// strict: only "e" confirms; every other key (Enter included) cancels.
	strict bool
	// alt is an optional third button (e.g. "Kaydetme") between confirm and cancel.
	alt    string
	altKey string // default "d"
	onAlt  func() tea.Cmd
}

func (c *confirmBox) key(m *Model, key string) tea.Cmd {
	if c.alt != "" {
		ak := c.altKey
		if ak == "" {
			ak = "d"
		}
		switch key {
		case "enter", "e", "y":
			m.confirm = nil
			return c.onYes()
		case ak, strings.ToUpper(ak):
			m.confirm = nil
			if c.onAlt != nil {
				return c.onAlt()
			}
			return nil
		case "esc", "h", "q":
			m.confirm = nil
		}
		return nil
	}
	if c.strict {
		m.confirm = nil
		if key == "e" || key == "E" {
			return c.onYes()
		}
		return nil
	}
	switch key {
	case "enter", "e", "y":
		m.confirm = nil
		return c.onYes()
	case "esc", "h", "n", "q":
		m.confirm = nil
	}
	return nil
}

func (c *confirmBox) draw(m *Model, g *grid.Grid) {
	w := min(64, g.W-4)
	lines := wrapText(c.text, w-6)
	h := len(lines) + 6
	x, y := (g.W-w)/2, (g.H-h)/2
	g.Box(x, y, w, h, cRed, cPanel, c.title, cRed)
	for i, l := range lines {
		g.Put(x+3, y+2+i, l, st(cFG), w-6)
	}
	by := y + h - 2
	addBtn := func(x0, x1 int, kind hitKind) {
		if x1 <= x0 {
			return
		}
		m.hits.add(hit{x: x0, y: by, w: x1 - x0, h: 1, kind: kind})
	}
	if c.alt != "" {
		ak := c.altKey
		if ak == "" {
			ak = "d"
		}
		x0 := x + 3
		bx := g.Put(x0, by, " Enter ", stbgb(cBG, cRed, true), 0)
		bx = g.Put(bx+1, by, c.yes, st(cFG), 0)
		addBtn(x0, bx, hitConfirmYes)
		x1 := bx + 4
		chip := " " + ak + " "
		bx = g.Put(x1, by, chip, stbgb(cBG, cAccent, true), 0)
		bx = g.Put(bx+1, by, c.alt, st(cFG), 0)
		addBtn(x1, bx, hitConfirmAlt)
		x2 := bx + 4
		bx = g.Put(x2, by, " Esc ", stbgb(cFG, cKey, true), 0)
		bx = g.Put(bx+1, by, "Vazgeç", st(cDim), 0)
		addBtn(x2, bx, hitConfirmNo)
		keybar(g, g.H-1, []keyHint{{"Enter", c.yes}, {strings.ToUpper(ak), c.alt}, {"Esc", "Vazgeç"}}, "", &m.hits)
		return
	}
	if c.strict {
		x0 := x + 3
		bx := g.Put(x0, by, " e ", stbgb(cBG, cRed, true), 0)
		bx = g.Put(bx+1, by, c.yes, st(cFG), 0)
		addBtn(x0, bx, hitConfirmYes)
		x1 := bx + 4
		bx = g.Put(x1, by, " h / Esc ", stbgb(cBG, cAccent, true), 0)
		bx = g.Put(bx+1, by, "Vazgeç", stb(cFG), 0)
		addBtn(x1, bx, hitConfirmNo)
		keybar(g, g.H-1, []keyHint{{"e", c.yes}, {"h / Esc", "Vazgeç"}}, "", &m.hits)
		return
	}
	x0 := x + 3
	bx := g.Put(x0, by, " Enter ", stbgb(cBG, cRed, true), 0)
	bx = g.Put(bx+1, by, c.yes, st(cFG), 0)
	addBtn(x0, bx, hitConfirmYes)
	x1 := bx + 4
	bx = g.Put(x1, by, " Esc ", stbgb(cFG, cKey, true), 0)
	bx = g.Put(bx+1, by, "Vazgeç", st(cDim), 0)
	addBtn(x1, bx, hitConfirmNo)
	keybar(g, g.H-1, []keyHint{{"Enter", c.yes}, {"Esc", "Vazgeç"}}, "", &m.hits)
}

// ---- folder picker (m: move) ----

type folderPicker struct {
	acct    int
	from    string
	env     backend.Envelope
	folders []backend.Folder
	cur     int
}

func newFolderPicker(m *Model, ai int, from string, e backend.Envelope) *folderPicker {
	p := &folderPicker{acct: ai, from: from, env: e}
	for _, f := range m.accts[ai].Folders {
		if f.Name != from && f.Role != backend.RoleFlagged && f.Role != backend.RoleAll {
			p.folders = append(p.folders, f)
		}
	}
	return p
}

func (p *folderPicker) key(m *Model, key string) tea.Cmd {
	switch key {
	case "esc", "q":
		m.picker = nil
	case "up":
		if p.cur > 0 {
			p.cur--
		}
	case "down":
		if p.cur < len(p.folders)-1 {
			p.cur++
		}
	case "enter":
		m.picker = nil
		if p.cur >= len(p.folders) {
			return nil
		}
		dest := p.folders[p.cur]
		if m.isInboxFolder(p.acct, p.from) && isUserFolder(dest) {
			return tea.Batch(m.copyToFolder(p.acct, p.from, p.env, dest.Name, dest.Label()), m.previewCmd())
		}
		m.removeEnv(p.acct, p.from, p.env.UID)
		m.toasts.MarkRead(p.acct, p.from, p.env.UID)
		id := m.undo.Push(undo.Entry{Op: undo.OpMove, Account: p.acct, From: p.from, Trash: dest.Name, OrigUID: p.env.UID, MessageID: p.env.MessageID, Subject: p.env.Subject, Data: p.env})
		m.linkUndoToast(id, m.toasts.Push(toast.Deleted, "Taşındı:", strings.TrimSpace(dest.Label()), m.opt.Now(), 0))
		return tea.Batch(m.moveCmd(p.acct, p.from, p.env, dest.Name, "move", id), m.previewCmd())
	}
	return nil
}

func (p *folderPicker) draw(m *Model, g *grid.Grid) {
	w := 40
	h := min(len(p.folders)+4, g.H-6)
	x, y := (g.W-w)/2, (g.H-h)/2
	g.Box(x, y, w, h, cBorderF, cRaised, "Klasöre taşı", cAccent)
	for i, f := range p.folders {
		if i >= h-4 {
			break
		}
		bg := cRaised
		if i == p.cur {
			bg = cSelFocus
		}
		g.Fill(x+1, y+2+i, w-2, 1, bg)
		g.Put(x+3, y+2+i, strings.TrimSpace(f.Label()), stbgb(cFG, bg, i == p.cur), w-6)
	}
	keybar(g, g.H-1, []keyHint{{"↑↓", "Seç"}, {"Enter", "Taşı"}, {"Esc", "Vazgeç"}}, "", &m.hits)
}

// ---- blocked senders screen (Ctrl+B) ----

type blockScreen struct{ cur int }

func (b *blockScreen) key(m *Model, key string) tea.Cmd {
	all := m.opt.Blocks.All()
	switch key {
	case "esc", "q", "ctrl+b":
		m.blocks = nil
	case "up":
		if b.cur > 0 {
			b.cur--
		}
	case "down":
		if b.cur < len(all)-1 {
			b.cur++
		}
	case "enter", "delete", "x", "backspace":
		if b.cur < len(all) {
			e := all[b.cur]
			m.opt.Blocks.Remove(e.Account, e.Kind, e.Value)
			m.saveBlocks()
			m.toastInfo(toast.Success, "Engel kaldırıldı:", e.Display())
			if b.cur >= len(all)-1 && b.cur > 0 {
				b.cur--
			}
		}
	}
	return nil
}

func (b *blockScreen) draw(m *Model, g *grid.Grid) {
	all := m.opt.Blocks.All()
	w := min(100, g.W-4)
	h := min(max(len(all)+8, 12), g.H-4)
	x, y := (g.W-w)/2, (g.H-h)/2-1
	g.Box(x, y, w, h, cBorderF, cPanel, "Engellenenler", cAccent)
	g.RPut(x+w-3, y, " Ctrl+B veya Esc: kapat ", st(cDim))
	g.Put(x+3, y+1, "Bu gönderenlerden gelen yeni postalar sessizce Spam klasörüne taşınır.", st(cFaint), w-6)
	cols := []int{x + 3, x + 3 + 34, x + 3 + 34 + 12, x + 3 + 34 + 12 + 34}
	hy := y + 3
	g.Put(cols[0], hy, "Adres / alan adı", stb(cAccent), 0)
	g.Put(cols[1], hy, "Tür", stb(cAccent), 0)
	g.Put(cols[2], hy, "Hesap", stb(cAccent), 0)
	g.Put(cols[3], hy, "Tarih", stb(cAccent), 0)
	g.Put(x+3, hy+1, strings.Repeat("─", w-6), st(cBorder), 0)
	if len(all) == 0 {
		g.Put(x+3, hy+3, "Henüz kimseyi engellemedin. Listede bir iletide b (adres) veya B (alan adı) ile engelleyebilirsin.", st(cDim), w-6)
	}
	maxRows := h - 7
	start := 0
	if b.cur >= maxRows {
		start = b.cur - maxRows + 1
	}
	for i := start; i < len(all) && i-start < maxRows; i++ {
		e := all[i]
		ry := hy + 2 + i - start
		bg := ""
		if i == b.cur {
			g.Fill(x+1, ry, w-2, 1, cSelFocus)
			bg = cSelFocus
		}
		g.Put(cols[0], ry, e.Display(), grid.Style{FG: cFG, BG: bg, Bold: i == b.cur}, 34)
		kc := cDim
		if e.Kind == blocklist.Domain {
			kc = cYellow
		}
		g.Put(cols[1], ry, e.Label(), grid.Style{FG: kc, BG: bg}, 10)
		g.Put(cols[2], ry, m.accountName(e.Account), grid.Style{FG: cDim, BG: bg}, 32)
		g.Put(cols[3], ry, mailmsg.ShortDay(e.Added), grid.Style{FG: cDim, BG: bg}, x+w-2-cols[3])
	}
	keybar(g, g.H-1, []keyHint{{"↑↓", "Seç"}, {"Enter / x", "Engeli kaldır"}, {"Esc", "Kapat"}}, "", &m.hits)
}

func (m *Model) accountName(email string) string {
	for _, a := range m.accts {
		if strings.EqualFold(a.Cfg.Email, email) {
			return a.Cfg.Name + " · " + a.Cfg.Email
		}
	}
	return email
}

// ---- help ("?") ----

func (m *Model) drawHelp(g *grid.Grid) {
	g.Dim(cDimText, map[string]bool{cPanel: true}, cBG)
	dw := min(124, g.W-4)
	dh := min(36, g.H-3)
	dx, dy := (g.W-dw)/2, max(0, (g.H-dh)/2-1)
	g.Box(dx, dy, dw, dh, cBorderF, cPanel, "Kısayollar", cAccent)
	g.RPut(dx+dw-3, dy, " ? veya Esc: kapat ", st(cDim))
	type grp struct {
		head  string
		items [][2]string
	}
	cols := [][]grp{
		{{"Gezinme", [][2]string{{"↑ ↓", "Gez (sağda önizlenir)"}, {"Enter / →", "İletiye gir (grup başlığında → genişletir)"}, {"o", "Tarayıcıda aç"}, {"t", "Sekmede aç (çift tık ile aynı)"}, {"←", "Klasör paneli (→ ile listeye dön)"}, {"Shift+G", "Gönderene göre grupla / düz (her panelden)"}, {"d", "Grup başlığında o gönderenin tüm maillerini sil"}, {"m", "Çöp'te grup başlığında tümünü Gelen Kutusu'na geri yükle"}, {"→ / ←", "Grubu, hesabı aç / kapat"}, {"Tab", "Sonraki sekme"}, {"Alt+1-9", "Sekmeye git"}, {"Ctrl+W", "Sekmeyi kapat"}}},
			{"Arama", [][2]string{{"/ · Ctrl+F", "Ara (yazdıkça süzülür)"}, {"Tab", "Bu klasör / tüm klasörler / hesaplar"}, {"kimden: ekli", "okunmamış kaydedilen bugün dün bu hafta bu ay"}, {"Esc", "Temizle, sonra kapat"}}},
			{"İletinin içinde", [][2]string{{"↑↓ PgUp/Dn", "Kaydır"}, {"] / [", "Sonraki / önceki ileti (okundu işaretler)"}, {"Enter", "Yanıtla (yeni sekme)"}, {"Esc", "Listeye dön"}, {"e", "Mail ↔ Ekler ve Bağlantılar sekmesi"}, {"Shift+E", "İletiyi doğrudan Ekler'de aç"}, {"↑ ↓ / 1-9", "(Ekler) satır seç / bağlantı no"}, {"Enter", "(Ekler) aç: bağlantı tarayıcıda, ek kendi programında"}, {"s", "Eki İndirilenler'e kaydet"}, {"Esc", "(Ekler) Mail sekmesine dön"}, {"c", "Metnin tamamını kopyala"}, {"C", "Gönderenin adresini kopyala"}, {"w", "Bağlantılar: aç / kopyala"}, {"k", "Sonraya kaydet"}}}},
		{{"İleti", [][2]string{{"n", "Yeni ileti"}, {"r", "Yanıtla"}, {"a / R", "Tümüne yanıtla"}, {"f", "İlet"}, {"k", "Sonraya kaydet / kaldır"}, {"e", "Ekler"}, {"d / Delete", "Çöpe at (Çöp'te: kalıcı)"}, {"Shift+D", "Çöp/Spam'i boşalt · diğer klasörler: hepsi çöpe"}, {"u", "Geri al"}, {"i", "Okundu / okunmadı (Okunanlar'a geçer)"}, {"m", "Klasöre taşı"}, {"b / B", "Engelle: kişi / alan adı"}}},
			{"Hesaplar", [][2]string{{"F2-F12", "Hesaba geç (F2 = 1. hesap)"}, {"F1", "Tüm Gelen Kutuları"}, {"Ctrl+A", "Hesap seçici (yazarken değil)"}, {"Alt+N", "Hesap satırında yeni klasör"}, {"+", "Hesap ekle"}}},
			{"Genel", [][2]string{{"g / x", "Bildirim: göster / kapat"}, {"Ctrl+B", "Engellenenler"}, {"?", "Bu ekran"}, {"q / Ctrl+C", "Çıkış (Ctrl+C: seçim yoksa)"}}}},
		{{"Yazarken", [][2]string{{"Ctrl+S", "Gönder (arka planda)"}, {"Tab / Shift+Tab", "Sonraki / önceki alan"}, {"↑ ↓", "Alanlar arası (metinde ilk/son satırda)"}, {"Ctrl+↑ / Ctrl+↓", "Önceki / sonraki alan (yedek)"}, {"Ctrl+F", "Gönderen hesabı"}, {"Ctrl+E", "Ek ekle"}, {"Esc", "Kapat (taslak sorulur)"}, {"Ctrl+K", "Gönderen hesabı değiştir"}}},
			{"Kopyala / yapıştır", [][2]string{{"Shift+oklar", "Seç (Shift+Home / End da)"}, {"Ctrl+A", "Alandaki her şeyi seç"}, {"Ctrl+C", "Seçileni kopyala (çıkmaz)"}, {"Ctrl+X", "Seçileni kes"}, {"Ctrl+V", "Yapıştır"}, {"Ctrl+Z / Y", "Geri al / yinele"}}},
			{"Fare", [][2]string{{"Tıkla", "Seç (çift tık: sekmede)"}, {"Okuma başlığı", "Tıkla: iletiye gir"}, {"Tekerlek", "Listeyi / metni kaydır"}, {"Sürükle", "Metni seç: bırakınca kopyalanır"}, {"2 / 3 tık", "Kelime / paragraf seç"}, {"Sağ tık", "Metinde kopyala, alanda yapıştır"}}}},
	}
	cw := (dw - 6) / 3
	for ci, groups := range cols {
		x := dx + 3 + ci*(cw+1)
		y := dy + 2
		for _, gr := range groups {
			if y >= dy+dh-1 {
				break
			}
			g.Put(x, y, gr.head, stb(cAccent), 0)
			y++
			g.Put(x, y, strings.Repeat("─", cw-2), st(cBorder), 0)
			y++
			for _, it := range gr.items {
				if y >= dy+dh-1 {
					break
				}
				g.Put(x, y, " "+it[0]+" ", stbgb(cFG, cKey, true), 0)
				g.Put(x+14, y, it[1], st(cDim), cw-15)
				y++
			}
			y++
		}
	}
	// Console fonts make O/0, S/5, B/8 look alike: say which are letters.
	g.Put(dx+3, dy+dh-2, "Ctrl ile basılanlar harftir (Ctrl+S: S harfi, Ctrl+B: B harfi, Ctrl+A: A harfi); Alt+1-9 ve 0-9 rakamdır.", st(cFaint), dw-6)
	keybar(g, g.H-1, []keyHint{{"?", "Kapat"}, {"Esc", "Kapat"}}, "", &m.hits)
}

var _ = fmt.Sprint
