package ui

import (
	"errors"
	"runtime"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/grid"
	"termail/internal/htmltext"
	"termail/internal/i18n"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

// linksKey opens the link list of the shown mail. The natural "l" is taken
// (it moves between attachments, like →), so the list uses "w" (web).
const linksKey = "w"

// viewText is what the reader shows: the text with numbered link markers.
func viewText(p *mailmsg.Parsed) string {
	if p.View != "" {
		return p.View
	}
	return p.Text
}

// drawBodyLine draws one wrapped body line; link markers ("[n]") are drawn
// small and dim in the accent colour. Every marker (and the word right
// before it, i.e. the end of the link text) becomes a click target.
func (m *Model) drawBodyLine(g *grid.Grid, x, y int, line string, fg string, maxw int, links []htmltext.Link) {
	if !strings.ContainsRune(line, htmltext.MarkRune) {
		g.Put(x, y, line, st(fg), maxw)
		return
	}
	end := x + maxw
	cx := x
	parts := strings.Split(line, string(htmltext.MarkRune))
	for i, part := range parts {
		if cx >= end {
			return
		}
		if i == 0 {
			cx = g.Put(cx, y, part, st(fg), end-cx)
			continue
		}
		cx++ // the marker character itself shows as a space
		if cx >= end {
			return
		}
		mk, rest := part, ""
		if j := strings.IndexByte(part, ']'); j >= 0 {
			mk, rest = part[:j+1], part[j+1:]
		}
		n, _ := strconv.Atoi(strings.Trim(mk, "[]"))
		// the word before the marker is the visible end of the link text
		prev := parts[i-1]
		if j := strings.IndexByte(prev, ']'); i > 1 && j >= 0 {
			prev = prev[j+1:] // text between two markers
		}
		wordStart := cx - 1 - grid.Width(prev)
		if k := strings.LastIndexByte(prev, ' '); k >= 0 {
			wordStart = cx - 1 - grid.Width(prev[k+1:])
		}
		cx = g.Put(cx, y, mk, st(cLinkMark), end-cx)
		if n >= 1 && n <= len(links) {
			m.hits.add(hit{x: max(x, wordStart), y: y, w: cx - max(x, wordStart), h: 1, kind: hitLink, s: links[n-1].URL})
		}
		if rest != "" && cx < end {
			cx = g.Put(cx, y, rest, st(fg), end-cx)
		}
	}
}

// ---- the "Bağlantılar" picker ----

type linkPicker struct {
	links []htmltext.Link
	cur   int
	top   int
	num   string // digits typed so far (lists with more than 9 links)
}

func (m *Model) openLinks(p *mailmsg.Parsed) tea.Cmd {
	if p == nil {
		m.toastInfo(toast.Info, "", i18n.T("İleti henüz yükleniyor."))
		return nil
	}
	if len(p.Links) == 0 {
		m.toastInfo(toast.Info, "", i18n.T("Bu iletide bağlantı yok."))
		return nil
	}
	m.links = &linkPicker{links: p.Links}
	return nil
}

func (lp *linkPicker) key(m *Model, key string) tea.Cmd {
	n := len(lp.links)
	switch key {
	case "esc", "q", linksKey:
		m.links = nil
	case "up", "k":
		lp.num = ""
		lp.cur = max(0, lp.cur-1)
	case "down", "j":
		lp.num = ""
		lp.cur = min(n-1, lp.cur+1)
	case "pgup":
		lp.cur = max(0, lp.cur-10)
	case "pgdown":
		lp.cur = min(n-1, lp.cur+10)
	case "home":
		lp.cur = 0
	case "end":
		lp.cur = n - 1
	case "backspace":
		if lp.num != "" {
			lp.num = lp.num[:len(lp.num)-1]
		}
	case "enter":
		i := lp.cur
		if lp.num != "" {
			v, _ := strconv.Atoi(lp.num)
			lp.num = ""
			if v < 1 || v > n {
				m.toastInfo(toast.Info, "", i18n.T("Bu numarada bağlantı yok."))
				return nil
			}
			i = v - 1
		}
		return lp.open(m, i)
	case "y":
		copyTextFn(lp.links[lp.cur].URL)
		m.toastInfo(toast.Success, "", i18n.T("Kopyalandı"))
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			lp.num += key
			v, _ := strconv.Atoi(lp.num)
			if v >= 1 && v <= n {
				lp.cur = v - 1
			}
			// open at once when no longer number can start with these digits
			if v >= 1 && v <= n && v*10 > n {
				lp.num = ""
				return lp.open(m, v-1)
			}
			if v > n || v == 0 && len(lp.num) > 1 {
				lp.num = ""
				m.toastInfo(toast.Info, "", i18n.T("Bu numarada bağlantı yok."))
			}
		}
	}
	return nil
}

func (lp *linkPicker) open(m *Model, i int) tea.Cmd {
	lp.cur = i
	m.links = nil
	return m.openLink(lp.links[i].URL)
}

// openLink opens a link from a mail in the default browser (after the same
// safety check every time) and says so in a toast.
func (m *Model) openLink(u string) tea.Cmd {
	safe, ok := htmltext.Normalize(u)
	if !ok {
		m.toastInfo(toast.Info, "", i18n.T("Bu bağlantı güvenli değil; açılmadı."))
		return nil
	}
	m.toastInfo(toast.Info, "", i18n.T("Tarayıcıda açılıyor…"))
	if err := openURLFn(safe); err != nil {
		m.toastErr("open link", errors.New("tarayıcı açılamadı; bağlantıyı y ile kopyalayabilirsin"))
	}
	return nil
}

func (lp *linkPicker) draw(m *Model, g *grid.Grid) {
	g.Dim(cDimText, map[string]bool{cPanel: true}, cBG)
	w := min(96, g.W-4)
	rows := min(len(lp.links), max(3, g.H-12))
	h := rows + 7
	x, y := (g.W-w)/2, max(1, (g.H-h)/2-1)
	m.hits.add(hit{x: 0, y: 0, w: g.W, h: g.H, kind: hitModal})
	g.Box(x, y, w, h, cBorderF, cPanel, i18n.T("Bağlantılar"), cAccent)
	g.RPut(x+w-3, y, " Esc: kapat ", st(cDim))
	if lp.cur < lp.top {
		lp.top = lp.cur
	}
	if lp.cur >= lp.top+rows {
		lp.top = lp.cur - rows + 1
	}
	numW := len(strconv.Itoa(len(lp.links))) + 2
	for i := lp.top; i < len(lp.links) && i < lp.top+rows; i++ {
		l := lp.links[i]
		yy := y + 2 + i - lp.top
		bg := cPanel
		if i == lp.cur {
			bg = cSelFocus
			g.Fill(x+1, yy, w-2, 1, bg)
		}
		m.hits.add(hit{x: x + 1, y: yy, w: w - 2, h: 1, kind: hitLinkRow, idx: i})
		cx := g.Put(x+2, yy, padLeft("["+strconv.Itoa(i+1)+"]", numW), stbg(cLinkMark, bg), 0) + 1
		dom := htmltext.Domain(l.URL)
		warn := htmltext.Mismatch(l.Text, l.URL)
		right := " — " + dom
		if warn {
			right += i18n.T(" · Dikkat: farklı adrese gidiyor")
		}
		rw := min(grid.Width(right), w-4-(cx-x)-10)
		text := l.Text
		if text == "" {
			text = htmltext.Shorten(l.URL, 60)
		}
		tx := g.Put(cx, yy, text, stbgb(cFG, bg, i == lp.cur), x+w-2-cx-rw)
		dfg := cDim
		if warn {
			dfg = cYellow
		}
		g.Put(tx, yy, right, stbg(dfg, bg), x+w-2-tx)
	}
	fy := y + h - 4
	g.Put(x+1, fy, strings.Repeat("─", w-2), st(cBorder), 0)
	if lp.cur < len(lp.links) {
		g.Put(x+2, fy+1, "Adres: "+htmltext.Shorten(lp.links[lp.cur].URL, w-12), st(cDim), w-4)
	}
	hint := i18n.T("↑↓ Enter: tarayıcıda aç · 1-9: doğrudan aç · y: adresi kopyala · Esc: kapat")
	if len(lp.links) > 9 {
		hint = i18n.T("↑↓ Enter: aç · numarayı yazıp Enter · y: kopyala · Esc: kapat")
	}
	if lp.num != "" {
		hint = "Numara: " + lp.num + i18n.T(" · Enter: aç")
	}
	g.Put(x+2, fy+2, hint, st(cFaint), w-4)
}

func padLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}

// ---- opening links safely ----

// cmdEscape protects a (validated) address for cmd.exe: every character
// cmd treats specially gets a ^ in front, so "&calc" stays part of the link.
func cmdEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`^&|<>()%!"`, r) {
			b.WriteByte('^')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// browserCommands lists the programs tried (in order) to open u. The link is
// checked first: only http, https and mailto, no spaces, quotes or control
// characters; it is always passed as one separate argument, never through a
// shell, except the cmd fallback where it is caret-escaped.
func browserCommands(goos, u string) ([][]string, error) {
	safe, ok := htmltext.Normalize(u)
	if !ok {
		return nil, errors.New("güvensiz bağlantı")
	}
	switch goos {
	case "windows":
		return [][]string{
			{"rundll32", "url.dll,FileProtocolHandler", safe},
			{"cmd", "/c", "start", "", cmdEscape(safe)},
		}, nil
	case "darwin":
		return [][]string{{"open", safe}}, nil
	default:
		return [][]string{{"xdg-open", safe}}, nil
	}
}

var _ = runtime.GOOS
