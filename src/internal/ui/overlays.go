package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"termail/internal/backend"
	"termail/internal/blocklist"
	"termail/internal/grid"
	"termail/internal/i18n"
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
		bx = g.Put(bx+1, by, i18n.T("Vazgeç"), st(cDim), 0)
		addBtn(x2, bx, hitConfirmNo)
		keybar(g, g.H-1, []keyHint{{"Enter", c.yes}, {strings.ToUpper(ak), c.alt}, {"Esc", i18n.T("Vazgeç")}}, "", &m.hits)
		return
	}
	if c.strict {
		x0 := x + 3
		bx := g.Put(x0, by, " e ", stbgb(cBG, cRed, true), 0)
		bx = g.Put(bx+1, by, c.yes, st(cFG), 0)
		addBtn(x0, bx, hitConfirmYes)
		x1 := bx + 4
		bx = g.Put(x1, by, " h / Esc ", stbgb(cBG, cAccent, true), 0)
		bx = g.Put(bx+1, by, i18n.T("Vazgeç"), stb(cFG), 0)
		addBtn(x1, bx, hitConfirmNo)
		keybar(g, g.H-1, []keyHint{{"e", c.yes}, {"h / Esc", i18n.T("Vazgeç")}}, "", &m.hits)
		return
	}
	x0 := x + 3
	bx := g.Put(x0, by, " Enter ", stbgb(cBG, cRed, true), 0)
	bx = g.Put(bx+1, by, c.yes, st(cFG), 0)
	addBtn(x0, bx, hitConfirmYes)
	x1 := bx + 4
	bx = g.Put(x1, by, " Esc ", stbgb(cFG, cKey, true), 0)
	bx = g.Put(bx+1, by, i18n.T("Vazgeç"), st(cDim), 0)
	addBtn(x1, bx, hitConfirmNo)
	keybar(g, g.H-1, []keyHint{{"Enter", c.yes}, {"Esc", i18n.T("Vazgeç")}}, "", &m.hits)
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
		m.linkUndoToast(id, m.toasts.Push(toast.Deleted, i18n.T("Taşındı:"), strings.TrimSpace(dest.Label()), m.opt.Now(), 0))
		return tea.Batch(m.moveCmd(p.acct, p.from, p.env, dest.Name, "move", id), m.previewCmd())
	}
	return nil
}

func (p *folderPicker) draw(m *Model, g *grid.Grid) {
	w := 40
	h := min(len(p.folders)+4, g.H-6)
	x, y := (g.W-w)/2, (g.H-h)/2
	g.Box(x, y, w, h, cBorderF, cRaised, i18n.T("Klasöre taşı"), cAccent)
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
	keybar(g, g.H-1, []keyHint{{"↑↓", i18n.T("Seç")}, {"Enter", i18n.T("Taşı")}, {"Esc", i18n.T("Vazgeç")}}, "", &m.hits)
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
			m.toastInfo(toast.Success, i18n.T("Engel kaldırıldı:"), e.Display())
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
	g.Box(x, y, w, h, cBorderF, cPanel, i18n.T("Engellenenler"), cAccent)
	g.RPut(x+w-3, y, " Ctrl+B veya Esc: kapat ", st(cDim))
	g.Put(x+3, y+1, i18n.T("Bu gönderenlerden gelen yeni postalar sessizce Spam klasörüne taşınır."), st(cFaint), w-6)
	cols := []int{x + 3, x + 3 + 34, x + 3 + 34 + 12, x + 3 + 34 + 12 + 34}
	hy := y + 3
	g.Put(cols[0], hy, i18n.T("Adres / alan adı"), stb(cAccent), 0)
	g.Put(cols[1], hy, i18n.T("Tür"), stb(cAccent), 0)
	g.Put(cols[2], hy, i18n.T("Hesap"), stb(cAccent), 0)
	g.Put(cols[3], hy, i18n.T("Tarih"), stb(cAccent), 0)
	g.Put(x+3, hy+1, strings.Repeat("─", w-6), st(cBorder), 0)
	if len(all) == 0 {
		g.Put(x+3, hy+3, i18n.T("Henüz kimseyi engellemedin. Listede bir iletide b (adres) veya B (alan adı) ile engelleyebilirsin."), st(cDim), w-6)
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
	keybar(g, g.H-1, []keyHint{{"↑↓", i18n.T("Seç")}, {"Enter / x", i18n.T("Engeli kaldır")}, {"Esc", i18n.T("Kapat")}}, "", &m.hits)
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
	g.Box(dx, dy, dw, dh, cBorderF, cPanel, i18n.T("Kısayollar"), cAccent)
	g.RPut(dx+dw-3, dy, " ? veya Esc: kapat ", st(cDim))
	type grp struct {
		head  string
		items [][2]string
	}
	cols := [][]grp{
		{{i18n.T("Gezinme"), [][2]string{{"↑ ↓", i18n.T("Gez (sağda önizlenir)")}, {"Enter / →", i18n.T("İletiye gir (grup başlığında → genişletir)")}, {"o", i18n.T("Tarayıcıda aç")}, {"t", i18n.T("Sekmede aç (çift tık ile aynı)")}, {"←", i18n.T("Klasör paneli (→ ile listeye dön)")}, {"Shift+G", i18n.T("Gönderene göre grupla / düz (her panelden)")}, {"d", i18n.T("Grup başlığında o gönderenin tüm maillerini sil")}, {"m", i18n.T("Çöp'te grup başlığında tümünü Gelen Kutusu'na geri yükle")}, {"→ / ←", i18n.T("Grubu, hesabı aç / kapat")}, {"Tab", "Sonraki sekme"}, {"Alt+1-9", "Sekmeye git"}, {"Ctrl+W", "Sekmeyi kapat"}}},
			{i18n.T("Arama"), [][2]string{{"/ · Ctrl+F", i18n.T("Ara (yazdıkça süzülür)")}, {"Tab", i18n.T("Bu klasör / tüm klasörler / hesaplar")}, {"kimden: ekli", i18n.T("okunmamış kaydedilen bugün dün bu hafta bu ay")}, {"Esc", "Temizle, sonra kapat"}}},
			{i18n.T("İletinin içinde"), [][2]string{{"↑↓ PgUp/Dn", i18n.T("Kaydır")}, {"] / [", i18n.T("Sonraki / önceki ileti (okundu işaretler)")}, {"Enter", i18n.T("Yanıtla (yeni sekme)")}, {"Esc", i18n.T("Listeye dön")}, {"e", i18n.T("Mail ↔ Ekler ve Bağlantılar sekmesi")}, {"Shift+E", i18n.T("İletiyi doğrudan Ekler'de aç")}, {"↑ ↓ / 1-9", i18n.T("(Ekler) satır seç / bağlantı no")}, {"Enter", i18n.T("(Ekler) aç: bağlantı tarayıcıda, ek kendi programında")}, {"s", i18n.T("Eki İndirilenler'e kaydet")}, {"Esc", i18n.T("(Ekler) Mail sekmesine dön")}, {"c", i18n.T("Metnin tamamını kopyala")}, {"C", i18n.T("Gönderenin adresini kopyala")}, {"w", i18n.T("Bağlantılar: aç / kopyala")}, {"k", "Sonraya kaydet"}}}},
		{{i18n.T("İleti"), [][2]string{{"n", i18n.T("Yeni ileti")}, {"r", i18n.T("Yanıtla")}, {"a / R", i18n.T("Tümüne yanıtla")}, {"f", i18n.T("İlet")}, {"k", i18n.T("Sonraya kaydet / kaldır")}, {"e", i18n.T("Ekler")}, {"d / Delete", i18n.T("Çöpe at (Çöp'te: kalıcı)")}, {"Shift+D", i18n.T("Çöp/Spam'i boşalt · diğer klasörler: hepsi çöpe")}, {"u", i18n.T("Geri al")}, {"i", i18n.T("Okundu / okunmadı (Okunanlar'a geçer)")}, {"m", i18n.T("Klasöre taşı")}, {"b / B", i18n.T("Engelle: kişi / alan adı")}}},
			{i18n.T("Hesaplar"), [][2]string{{"F2-F12", i18n.T("Hesaba geç (F2 = 1. hesap)")}, {"F1", i18n.T("Tüm Gelen Kutuları")}, {"Ctrl+A", i18n.T("Hesap seçici (yazarken değil)")}, {"Alt+N", i18n.T("Hesap satırında yeni klasör")}, {"+", i18n.T("Hesap ekle")}}},
			{i18n.T("Genel"), [][2]string{{"g / x", i18n.T("Bildirim: göster / kapat")}, {"Ctrl+B", i18n.T("Engellenenler")}, {"L", "Dil / Language"}, {"?", "Bu ekran"}, {"q / Ctrl+C", i18n.T("Çıkış (Ctrl+C: seçim yoksa)")}}}},
		{{i18n.T("Yazarken"), [][2]string{{"Ctrl+S", i18n.T("Gönder (arka planda)")}, {"Tab / Shift+Tab", i18n.T("Sonraki / önceki alan")}, {"↑ ↓", i18n.T("Alanlar arası (metinde ilk/son satırda)")}, {"Ctrl+↑ / Ctrl+↓", i18n.T("Önceki / sonraki alan (yedek)")}, {"Ctrl+F", i18n.T("Gönderen hesabı")}, {"Ctrl+E", "Ek ekle"}, {"Esc", i18n.T("Kapat (taslak sorulur)")}, {"Ctrl+K", i18n.T("Gönderen hesabı değiştir")}}},
			{i18n.T("Kopyala / yapıştır"), [][2]string{{"Shift+oklar", i18n.T("Seç (Shift+Home / End da)")}, {"Ctrl+A", i18n.T("Alandaki her şeyi seç")}, {"Ctrl+C", i18n.T("Seçileni kopyala (çıkmaz)")}, {"Ctrl+X", i18n.T("Seçileni kes")}, {"Ctrl+V", i18n.T("Yapıştır")}, {"Ctrl+Z / Y", "Geri al / yinele"}}},
			{i18n.T("Fare"), [][2]string{{i18n.T("Tıkla"), i18n.T("Seç (çift tık: sekmede)")}, {i18n.T("Okuma başlığı"), i18n.T("Tıkla: iletiye gir")}, {i18n.T("Tekerlek"), i18n.T("Listeyi / metni kaydır")}, {i18n.T("Sürükle"), i18n.T("Metni seç: bırakınca kopyalanır")}, {i18n.T("2 / 3 tık"), i18n.T("Kelime / paragraf seç")}, {i18n.T("Sağ tık"), i18n.T("Metinde kopyala, alanda yapıştır")}}}},
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
	lang := "L  Dil: Türkçe  →  English"
	if i18n.Get() == i18n.EN {
		lang = "L  Language: English  →  Türkçe"
	}
	g.Put(dx+3, dy+dh-3, lang, stb(cAccent), dw-6)
	g.Put(dx+3, dy+dh-2, i18n.T("Ctrl ile basılanlar harftir (Ctrl+S: S harfi, Ctrl+B: B harfi, Ctrl+A: A harfi); Alt+1-9 ve 0-9 rakamdır."), st(cFaint), dw-6)
	keybar(g, g.H-1, []keyHint{{"L", i18n.T("Dil")}, {"?", i18n.T("Kapat")}, {"Esc", i18n.T("Kapat")}}, "", &m.hits)
}

var _ = fmt.Sprint
