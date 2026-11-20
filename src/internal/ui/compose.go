package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emersion/go-message/mail"

	"termail/internal/backend"
	"termail/internal/edit"
	"termail/internal/grid"
	"termail/internal/i18n"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

type composeMode int

const (
	composeNew composeMode = iota
	composeReply
	composeReplyAll
	composeForward
)

// Fields in navigation order (Tab / Shift+Tab cycle To → Cc → Subject →
// Body → Attachments; From is reached with Ctrl+F or ↑ from To).
const (
	fFrom = iota
	fTo
	fCc
	fSubject
	fBody
	fAttach
	fCount
)

func nextField(f int) int {
	if f >= fAttach || f < fTo {
		return fTo
	}
	return f + 1
}

func prevField(f int) int {
	if f <= fTo {
		return fAttach
	}
	return f - 1
}

type composer struct {
	mode       composeMode
	from       int
	dropdown   bool
	to, cc     edit.Editor
	subject    edit.Editor
	body       edit.Editor
	atts       []mailmsg.Attachment
	attSel     int
	focus      int
	orig       *bodyRef
	origEnv    backend.Envelope
	status     string
	statusFG   string
	sending    bool
	closing    bool
	noteSet    bool // status holds a short note (cleared by the next key)
	picker     *attachPicker
	tab        *tab
	initial    string
	w, h       int
	suggest    []recipEntry
	suggestCur int
}

func newInput(k edit.Kind, placeholder string) edit.Editor {
	in := edit.New(k)
	in.Placeholder = placeholder
	return in
}

// composeFromAcct is the From account for a new mail: the account of the
// selected or open message, or of the folder under the cursor.
func (m *Model) composeFromAcct() int {
	if len(m.accts) == 0 {
		return 0
	}
	if t := m.tabs[m.active]; t != nil && t.kind == tabRead && t.read != nil {
		if t.read.key.acct >= 0 && t.read.key.acct < len(m.accts) {
			return t.read.key.acct
		}
	}
	if m.focus == focusReader && m.pv != nil && m.pv.key.acct >= 0 && m.pv.key.acct < len(m.accts) {
		return m.pv.key.acct
	}
	if e, ok := m.selected(); ok && e.UID != 0 && !isGroupRow(e) {
		ai, _ := m.loc(e)
		if ai >= 0 && ai < len(m.accts) {
			return ai
		}
	}
	if m.focus == focusFolders {
		if r, ok := m.folderCursorRow(); ok {
			if r.kind == rowFolder || r.kind == rowAcct {
				if r.acct >= 0 && r.acct < len(m.accts) {
					return r.acct
				}
			}
		}
	}
	if m.cur >= 0 && m.cur < len(m.accts) {
		return m.cur
	}
	return 0
}

func (m *Model) openCompose(mode composeMode, orig *bodyRef, e backend.Envelope) tea.Cmd {
	if len(m.accts) == 0 {
		return nil
	}
	from := m.composeFromAcct()
	if orig != nil && orig.key.acct < len(m.accts) {
		from = orig.key.acct // answer from the account the mail arrived at
	}
	c := &composer{mode: mode, from: from, orig: orig, origEnv: e,
		to: newInput(edit.Address, i18n.T("alıcı@ornek.com, ikinci@ornek.com")), cc: newInput(edit.Address, ""), subject: newInput(edit.Single, "")}
	c.body = newInput(edit.Multi, i18n.T("İletini buraya yaz…"))
	title := i18n.T("Yeni ileti")
	var p *mailmsg.Parsed
	if orig != nil {
		p = m.body(orig.key)
		c.from = orig.key.acct
	}
	me := strings.ToLower(m.accts[c.from].Cfg.Email)
	switch mode {
	case composeReply, composeReplyAll:
		c.subject.SetValue(mailmsg.ReplySubject(e.Subject))
		var to []*mail.Address
		if p != nil && len(p.ReplyTo) > 0 {
			to = p.ReplyTo
		} else if p != nil && len(p.From) > 0 {
			to = p.From
		} else if e.FromAddr != "" {
			to = []*mail.Address{{Name: e.From, Address: e.FromAddr}}
		}
		var cc []*mail.Address
		if mode == composeReplyAll && p != nil {
			seen := map[string]bool{me: true}
			for _, a := range to {
				seen[strings.ToLower(a.Address)] = true
			}
			for _, a := range append(append([]*mail.Address{}, p.To...), p.Cc...) {
				if !seen[strings.ToLower(a.Address)] {
					seen[strings.ToLower(a.Address)] = true
					cc = append(cc, a)
				}
			}
		}
		c.to.SetValue(mailmsg.FormatAddresses(to))
		c.cc.SetValue(mailmsg.FormatAddresses(cc))
		c.focus = fBody
		title = i18n.T("Yanıt: ") + trimSubj(e.Subject)
	case composeForward:
		c.subject.SetValue(mailmsg.ForwardSubject(e.Subject))
		if p != nil {
			c.atts = append(c.atts, p.Attachments...)
		}
		c.focus = fTo
		title = i18n.T("İlet: ") + trimSubj(e.Subject)
	default:
		c.focus = fTo
	}
	c.initial = c.snapshot()
	t := &tab{kind: tabCompose, title: title, compose: c}
	c.tab = t
	m.tabs = append(m.tabs, t)
	m.active = len(m.tabs) - 1
	c.resize(m.w, m.h)
	cmd := c.applyFocus()
	var load tea.Cmd
	if orig != nil {
		load = m.loadBody(orig.key)
	}
	return tea.Batch(cmd, load, m.prewarm(c.from))
}

// prewarm opens and signs in the outgoing connection in the background, so
// Ctrl+S does not wait for it.
func (m *Model) prewarm(ai int) tea.Cmd {
	if ai >= len(m.accts) || m.accts[ai].B == nil || m.opt.Demo {
		return nil
	}
	b := m.accts[ai].B
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		if err := b.Prewarm(ctx); err != nil {
			m.log.Printf("prewarm: %v", err)
		}
		return nil
	}
}

func (c *composer) snapshot() string {
	return c.to.Value() + "\x00" + c.cc.Value() + "\x00" + c.subject.Value() + "\x00" + c.body.Value()
}

func (c *composer) dirty() bool {
	return c.snapshot() != c.initial || len(c.atts) > 0 && c.mode != composeForward
}

func (c *composer) resize(w, h int) {
	c.w, c.h = w, h
	fw := w - 16 - 4 - 2
	for _, in := range []*edit.Editor{&c.to, &c.cc, &c.subject} {
		in.SetWidth(max(10, fw-2))
	}
	c.body.SetWidth(max(20, min(w-8, 110)))
}

func (c *composer) applyFocus() tea.Cmd {
	c.to.Blur()
	c.cc.Blur()
	c.subject.Blur()
	c.body.Blur()
	switch c.focus {
	case fTo:
		return c.to.Focus()
	case fCc:
		return c.cc.Focus()
	case fSubject:
		return c.subject.Focus()
	case fBody:
		return c.body.Focus()
	}
	return nil
}

func (c *composer) headerText() string {
	switch c.mode {
	case composeReply, composeReplyAll:
		return i18n.T("Yanıt yazılıyor")
	case composeForward:
		return i18n.T("İleti iletiliyor")
	}
	return i18n.T("Yeni ileti yazılıyor")
}

// field is the focused text field (nil on Gönderen / Ekler).
func (c *composer) field() *edit.Editor {
	switch c.focus {
	case fTo:
		return &c.to
	case fCc:
		return &c.cc
	case fSubject:
		return &c.subject
	case fBody:
		return &c.body
	}
	return nil
}

func (c *composer) fieldAt(idx int) *edit.Editor {
	f := c.focus
	c.focus = idx
	e := c.field()
	c.focus = f
	return e
}

func (c *composer) key(m *Model, k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if c.picker != nil {
		return c.picker.key(m, c, k)
	}
	if c.sending {
		return nil
	}
	if c.noteSet {
		c.status, c.noteSet = "", false
	}
	if c.dropdown {
		switch key {
		case "up", "left":
			c.from = (c.from - 1 + len(m.accts)) % len(m.accts)
		case "down", "right", "ctrl+f", "ctrl+k":
			c.from = (c.from + 1) % len(m.accts)
		case "enter", "esc", "tab":
			c.dropdown = false
		}
		return nil
	}
	if len(c.suggest) > 0 {
		switch key {
		case "up":
			if c.suggestCur > 0 {
				c.suggestCur--
			}
			return nil
		case "down":
			if c.suggestCur < len(c.suggest)-1 {
				c.suggestCur++
			}
			return nil
		case "enter", "tab":
			c.acceptSuggest()
			c.refreshSuggest(m)
			if key == "tab" {
				c.focus = nextField(c.focus)
				return c.applyFocus()
			}
			return nil
		case "esc":
			c.suggest, c.suggestCur = nil, 0
			return nil
		}
	}
	switch key {
	case "ctrl+s", "ctrl+enter":
		return c.send(m)
	case "esc":
		if f := c.field(); f != nil && f.HasSelection() {
			f.ClearSelection()
			return nil
		}
		return c.askClose(m)
	case "tab", "ctrl+down":
		c.focus = nextField(c.focus)
		c.suggest = nil
		return c.applyFocus()
	case "shift+tab", "ctrl+up":
		c.focus = prevField(c.focus)
		c.suggest = nil
		return c.applyFocus()
	case "ctrl+f":
		c.focus = fFrom
		c.applyFocus()
		c.dropdown = true
		return nil
	case "ctrl+k":
		if len(m.accts) > 0 {
			c.from = (c.from + 1) % len(m.accts)
		}
		return nil
	case "ctrl+e":
		p, cmd := newAttachPicker()
		c.picker = p
		return tea.Batch(p.in.Focus(), cmd)
	}
	switch c.focus {
	case fFrom:
		switch key {
		case "left":
			c.from = (c.from - 1 + len(m.accts)) % len(m.accts)
		case "right":
			c.from = (c.from + 1) % len(m.accts)
		case "enter", " ":
			c.dropdown = true
		case "down":
			c.focus = fTo
			return c.applyFocus()
		}
		return nil
	case fAttach:
		switch key {
		case "left":
			c.attSel = max(0, c.attSel-1)
		case "right":
			c.attSel = min(len(c.atts)-1, c.attSel+1)
		case "delete", "backspace":
			if c.attSel < len(c.atts) {
				c.atts = append(c.atts[:c.attSel], c.atts[c.attSel+1:]...)
				c.attSel = max(0, min(c.attSel, len(c.atts)-1))
				c.status = ""
			}
		case "enter":
			p, cmd := newAttachPicker()
			c.picker = p
			return tea.Batch(p.in.Focus(), cmd)
		case "up":
			c.focus = fBody
			return c.applyFocus()
		case "down":
			c.focus = fTo
			return c.applyFocus()
		}
		return nil
	case fTo, fCc, fSubject:
		switch key {
		case "enter", "down":
			c.focus = nextField(c.focus)
			return c.applyFocus()
		case "up":
			if c.focus == fTo {
				c.focus = fFrom
			} else {
				c.focus = prevField(c.focus)
			}
			return c.applyFocus()
		}
	case fBody:
		switch key {
		case "up":
			if c.body.AtFirstRow() {
				c.focus = fSubject
				return c.applyFocus()
			}
		case "down":
			if c.body.AtLastRow() {
				c.focus = fAttach
				return c.applyFocus()
			}
		}
	}
	if f := c.field(); f != nil {
		_, cmd := m.editKey(f, k)
		if c.focus == fTo || c.focus == fCc {
			c.refreshSuggest(m)
		} else {
			c.suggest, c.suggestCur = nil, 0
		}
		return cmd
	}
	return nil
}

func (c *composer) refreshSuggest(m *Model) {
	c.suggest, c.suggestCur = nil, 0
	if c.focus != fTo && c.focus != fCc {
		return
	}
	f := c.field()
	if f == nil {
		return
	}
	_, _, tok := addrToken(f.Value(), f.Cursor().Col)
	if strings.TrimSpace(tok) == "" {
		return
	}
	c.suggest = m.recipBook().suggest(tok, 8)
}

func (c *composer) acceptSuggest() {
	if c.suggestCur < 0 || c.suggestCur >= len(c.suggest) {
		c.suggest = nil
		return
	}
	f := c.field()
	if f == nil {
		return
	}
	col := f.Cursor().Col
	v := replaceAddrToken(f.Value(), col, formatRecip(c.suggest[c.suggestCur]))
	f.SetValue(v)
	f.CursorEnd()
	c.suggest, c.suggestCur = nil, 0
}

func (c *composer) askClose(m *Model) tea.Cmd {
	if !c.dirty() {
		m.removeTab(c.tab)
		return nil
	}
	m.confirm = &confirmBox{
		title:  "Taslak",
		text:   "Taslak olarak kaydedilsin mi?",
		yes:    i18n.T("Kaydet"),
		alt:    i18n.T("Kaydetme"),
		altKey: "d",
		onYes:  func() tea.Cmd { return c.saveDraftAndClose(m) },
		onAlt: func() tea.Cmd {
			m.removeTab(c.tab)
			return nil
		},
	}
	return nil
}

// fullBody returns the text with the quoted original / forwarded message.
func (c *composer) fullBody(m *Model) string {
	b := c.body.Value()
	if c.orig == nil {
		return b
	}
	p := m.body(c.orig.key)
	if p == nil {
		return b
	}
	switch c.mode {
	case composeReply, composeReplyAll:
		return strings.TrimRight(b, "\n") + "\n\n" + mailmsg.QuoteHeader(p) + "\n" + strings.Join(mailmsg.QuoteLines(p.Text), "\n") + "\n"
	case composeForward:
		hdr := "\n\n---------- İletilen ileti ----------\n" +
			i18n.T("Kimden: ") + mailmsg.FormatAddresses(p.From) + "\n" +
			i18n.T("Tarih: ") + mailmsg.LongDate(p.Date) + "\n" +
			i18n.T("Konu: ") + p.Subject + "\n" +
			i18n.T("Kime: ") + mailmsg.FormatAddresses(p.To) + "\n\n"
		return strings.TrimRight(b, "\n") + hdr + p.Text + "\n"
	}
	return b
}

func (c *composer) build(m *Model, requireTo bool) (*mailmsg.Outgoing, error) {
	a := m.accts[c.from]
	to, err := mailmsg.ParseAddresses(c.to.Value())
	if err != nil {
		return nil, errors.New(i18n.T("Kime alanındaki adreslerden biri geçersiz."))
	}
	cc, err := mailmsg.ParseAddresses(c.cc.Value())
	if err != nil {
		return nil, errors.New(i18n.T("Bilgi alanındaki adreslerden biri geçersiz."))
	}
	if requireTo && len(to)+len(cc) == 0 {
		return nil, errors.New(i18n.T("En az bir alıcı yaz."))
	}
	o := &mailmsg.Outgoing{From: &mail.Address{Name: a.Cfg.FullName, Address: a.Cfg.Email}, To: to, Cc: cc,
		Subject: c.subject.Value(), Body: c.fullBody(m), Attachments: c.atts}
	if c.orig != nil && (c.mode == composeReply || c.mode == composeReplyAll) {
		if p := m.body(c.orig.key); p != nil && p.MessageID != "" {
			o.InReplyTo = p.MessageID
			o.References = append(append([]string{}, p.References...), p.MessageID)
		}
	}
	return o, nil
}

func (c *composer) send(m *Model) tea.Cmd {
	o, err := c.build(m, true)
	if err != nil {
		c.status, c.statusFG = err.Error(), cRed
		return nil
	}
	if c.orig != nil && m.body(c.orig.key) == nil {
		c.status, c.statusFG = i18n.T("Asıl ileti henüz yüklenmedi, birkaç saniye sonra tekrar dene."), cYellow
		return nil
	}
	if names := riskyNames(c.atts); names != "" {
		m.confirm = &confirmBox{strict: true, title: i18n.T("Program dosyası ekli"),
			text: names + " bir program dosyası. Gmail, Outlook ve çoğu e-posta servisi bu tür ekleri güvenlik nedeniyle geri çevirir; ileti alıcıya ulaşmaz.\n\nPaylaşmak için dosyayı bir bulut klasörüne koyup bağlantısını göndermek daha iyi olur.\n\nYine de gönderilsin mi? e/h",
			yes:  i18n.T("Yine de gönder"), onYes: func() tea.Cmd { return c.dispatch(m, o) }}
		return nil
	}
	return c.dispatch(m, o)
}

// dispatch closes the compose tab at once and sends in the background. On
// failure the tab comes back with everything that was written.
func (c *composer) dispatch(m *Model, o *mailmsg.Outgoing) tea.Cmd {
	raw, err := mailmsg.Build(o)
	if err != nil {
		c.status, c.statusFG = i18n.T("İleti hazırlanamadı."), cRed
		m.logErr("build", err)
		return nil
	}
	a := m.accts[c.from]
	sent := ""
	if a.Cfg.Settings.SaveSent {
		sent = a.folderByRole(backend.RoleSent)
	}
	c.sending = true
	c.status = ""
	m.removeTab(c.tab)
	id := m.toasts.Push(toast.Info, "", i18n.T("Gönderiliyor…"), m.opt.Now(), 10*time.Minute)
	m.sendsPending++
	m.inflight[c] = true
	b := a.B
	from := a.Cfg.Email
	rcpts := o.Recipients()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
		defer cancel()
		return sendDoneMsg{c: c, toastID: id, err: b.Send(ctx, from, rcpts, raw, sent)}
	}
}

// riskyNames lists attached files that mail services refuse to deliver.
func riskyNames(atts []mailmsg.Attachment) string {
	var names []string
	for _, a := range atts {
		if mailmsg.Risky(a.Name, a.Data) {
			names = append(names, a.Name)
		}
	}
	return strings.Join(names, ", ")
}

func (c *composer) saveDraftAndClose(m *Model) tea.Cmd {
	if !c.dirty() {
		m.removeTab(c.tab)
		return nil
	}
	a := m.accts[c.from]
	drafts := a.folderByRole(backend.RoleDrafts)
	if drafts == "" {
		c.status, c.statusFG = i18n.T("Taslaklar klasörü bulunamadı; taslak kaydedilemedi. Ctrl+W ile yine de kapatabilirsin."), cYellow
		c.closing = true
		return nil
	}
	o, _ := c.build(m, false)
	if o == nil {
		o = &mailmsg.Outgoing{From: &mail.Address{Address: a.Cfg.Email}, Subject: c.subject.Value(), Body: c.fullBody(m)}
	}
	raw, err := mailmsg.Build(o)
	if err != nil {
		m.logErr("draft", err)
		return nil
	}
	c.sending = true
	c.status, c.statusFG = "Taslak kaydediliyor…", cAccent
	b := a.B
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, time.Minute)
		defer cancel()
		return sendDoneMsg{c: c, draft: true, err: b.Append(ctx, drafts, raw, []backend.Flag{backend.FlagDraft, backend.FlagSeen})}
	}
}

func (m *Model) onSendDone(d sendDoneMsg) tea.Cmd {
	c := d.c
	c.sending = false
	if d.draft {
		if d.err != nil {
			c.status, c.statusFG = "Taslak kaydedilemedi: "+friendly(d.err), cRed
			c.closing = true
			m.logErr("draft", d.err)
			return nil
		}
		m.removeTab(c.tab)
		m.toastInfo(toast.Success, "", "Taslak kaydedildi")
		return nil
	}
	m.sendsPending = max(0, m.sendsPending-1)
	delete(m.inflight, c)
	m.toasts.Remove(d.toastID)
	switch {
	case d.err == nil:
		m.rememberSent(c)
		m.toastInfo(toast.Success, "", i18n.T("Gönderildi"))
	case errors.Is(d.err, backend.ErrSentCopy):
		m.rememberSent(c)
		m.logErr("send", d.err)
		m.toastInfo(toast.Info, "", i18n.T("Gönderildi; yalnız kopyası Gönderilenler'e kaydedilemedi."))
	default:
		m.logErr("send", d.err)
		// bring the mail back exactly as it was, so nothing is lost
		c.status, c.statusFG = i18n.T("Gönderilemedi: ")+sendErrText(d.err)+i18n.T(" Düzeltip Ctrl+S ile tekrar gönderebilirsin."), cRed
		t := c.tab
		t.compose = c
		m.tabs = append(m.tabs, t)
		m.active = len(m.tabs) - 1
		m.toasts.Push(toast.Error, "", i18n.T("Gönderilemedi; ileti yeniden açıldı."), m.opt.Now(), 6*time.Second)
		return c.applyFocus()
	}
	return nil
}

// sendErrText explains a refused mail in plain Turkish.
func sendErrText(err error) string {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "security issue") || strings.Contains(s, "blocked") || strings.Contains(s, "virus") || strings.Contains(s, "executable"):
		return i18n.T("Karşı taraf iletiyi güvenlik nedeniyle geri çevirdi (genellikle program dosyası eki yüzünden).")
	case strings.Contains(s, "size") || strings.Contains(s, "too large") || strings.Contains(s, "552"):
		return i18n.T("İleti çok büyük; ekleri küçült.")
	case strings.Contains(s, "recipient") || strings.Contains(s, "mailbox unavailable") || strings.Contains(s, "550 5.1.1"):
		return i18n.T("Alıcı adresi bulunamadı; adresi kontrol et.")
	}
	return friendly(err)
}

func (c *composer) draw(m *Model, g *grid.Grid) {
	top := 2
	h := g.H - 3
	title := map[composeMode]string{composeNew: i18n.T("Yeni ileti"), composeReply: i18n.T("Yanıtla"), composeReplyAll: i18n.T("Tümüne yanıtla"), composeForward: i18n.T("İlet")}[c.mode]
	if c.subject.Value() != "" {
		title += " · " + c.subject.Value()
	}
	g.Box(0, top, g.W, h, cBorderF, cBG, title, cAccent)
	lx, fx := 3, 16
	fw := g.W - fx - 4
	field := func(y, idx int, label string) string {
		focus := c.focus == idx
		bg := cPanel
		lc := cDim
		if focus {
			bg, lc = cRaised, cAccent
		}
		g.Put(lx, y, label, grid.Style{FG: lc, Bold: focus}, 0)
		g.Fill(fx, y, fw, 1, bg)
		if idx != fFrom {
			m.hits.add(hit{x: lx, y: y, w: fx + fw - lx, h: 1, kind: hitField, idx: idx, ox: fx + 1, oy: y})
		}
		return bg
	}
	y := top + 2
	a := m.accts[c.from]
	bg := field(y, fFrom, i18n.T("Gönderen"))
	// clickable row + ‹ › arrows
	m.hits.add(hit{x: lx, y: y, w: fx + fw - lx, h: 1, kind: hitFromRow})
	ax := fx + 1
	g.Put(ax, y, "‹", stbg(cAccent, bg), 0)
	m.hits.add(hit{x: ax, y: y, w: 2, h: 1, kind: hitFromPrev})
	label := a.Cfg.Name + "  <" + a.Cfg.Email + ">"
	g.Put(ax+2, y, label, stbg(cFG, bg), fw-36)
	g.Put(fx+fw-22, y, "›", stbg(cAccent, bg), 0)
	m.hits.add(hit{x: fx + fw - 23, y: y, w: 3, h: 1, kind: hitFromNext})
	g.RPut(fx+fw-1, y, i18n.T("← → hesap değiştir"), stbg(cFaint, bg))
	gy := y
	y += 2
	var suggestY int
	for _, f := range []struct {
		idx   int
		label string
		in    *edit.Editor
		hint  string
	}{{fTo, i18n.T("Kime"), &c.to, ""}, {fCc, "Bilgi", &c.cc, i18n.T("isteğe bağlı")}, {fSubject, i18n.T("Konu"), &c.subject, ""}} {
		bg := field(y, f.idx, f.label)
		hw := 0
		if f.hint != "" && f.in.Value() == "" {
			hw = grid.Width(f.hint) + 1
		}
		f.in.Draw(g, fx+1, y, fw-2-hw, 1, editStyle(bg))
		if f.hint != "" && f.in.Value() == "" {
			g.RPut(fx+fw-1, y, f.hint, stbg(cFaint, bg))
		}
		if (f.idx == fTo || f.idx == fCc) && c.focus == f.idx && len(c.suggest) > 0 {
			suggestY = y
		}
		y += 2
	}
	if suggestY > 0 {
		c.drawSuggest(m, g, fx, suggestY, fw)
	}
	g.HLine(0, y, g.W, cBorder, "├", "┤")
	g.Put(3, y, i18n.T(" İleti "), stb(cAccent), 0)
	y += 2
	bottom := top + h - 6
	// quoted original below the editor (faint)
	var quote []string
	if c.orig != nil {
		if p := m.body(c.orig.key); p != nil {
			switch c.mode {
			case composeReply, composeReplyAll:
				quote = append(quote, "──── "+mailmsg.QuoteHeader(p)+" ────")
				for _, l := range wrapText(p.Text, g.W-12) {
					quote = append(quote, "│ "+l)
				}
			case composeForward:
				quote = append(quote, i18n.T("──── İletilen ileti: ")+mailmsg.Display(firstAddr(p.From))+", "+mailmsg.LongDate(p.Date)+" ────")
				for _, l := range wrapText(p.Text, g.W-12) {
					quote = append(quote, "│ "+l)
				}
			}
		} else {
			quote = []string{i18n.T("(asıl ileti yükleniyor…)")}
		}
	}
	avail := bottom - y
	qmax := min(len(quote), max(0, avail/3))
	if len(quote) > 0 && qmax < 2 {
		qmax = min(len(quote), 2)
	}
	bodyH := max(3, avail-qmax-func() int {
		if qmax > 0 {
			return 1
		}
		return 0
	}())
	bw := max(20, min(g.W-8, 110))
	m.hits.add(hit{x: lx + 1, y: y, w: g.W - lx - 4, h: bodyH, kind: hitField, idx: fBody, ox: lx + 1, oy: y})
	c.body.Draw(g, lx+1, y, bw, bodyH, editStyle(cBG))
	qy := y + bodyH + 1
	for i := 0; i < qmax && qy+i < bottom+1; i++ {
		l := quote[i]
		if i == qmax-1 && qmax < len(quote) {
			l = "│ …"
		}
		g.Put(lx+1, qy+i, l, st(cFaint), g.W-lx-4)
	}
	y = top + h - 4 // attachments sit below the text (Tab order: … Gövde → Ekler)
	bg = field(y, fAttach, i18n.T("Ekler"))
	ax = fx + 1
	if len(c.atts) == 0 {
		g.Put(ax, y, "yok", stbg(cFaint, bg), 0)
	}
	for i, at := range c.atts {
		txt := "▪ " + at.Name + " (" + mailmsg.HumanSize(len(at.Data)) + ")"
		if ax+grid.Width(txt) > fx+fw-18 {
			g.Put(ax, y, "…", stbg(cPurple, bg), 0)
			break
		}
		s := stbg(cPurple, bg)
		if c.focus == fAttach && i == c.attSel {
			s = stbgb(cFG, cSelFocus, true)
		}
		ax = g.Put(ax, y, txt, s, 0) + 4
	}
	hint := "Ctrl+E: ek ekle"
	if c.focus == fAttach && len(c.atts) > 0 {
		hint = i18n.T("Delete: çıkar · Enter: ekle")
	}
	g.RPut(fx+fw-1, y, hint, stbg(cFaint, bg))
	// status line
	status := c.status
	sfg := c.statusFG
	if status == "" {
		status, sfg = i18n.T("Ctrl+S ile gönder · Esc ile taslağa kaydedip kapat"), cFaint
	}
	g.Put(lx, top+h-2, status, st(sfg), g.W-lx-20)
	words := len(strings.Fields(c.body.Value()))
	g.RPut(g.W-3, top+h-2, fmtWords(words), st(cFaint))
	if c.dropdown {
		dw := min(56, fw)
		n := len(m.accts)
		g.Box(fx, gy+1, dw, n+2, cBorderF, cRaised, "", "")
		for i, ac := range m.accts {
			bg := cRaised
			if i == c.from {
				bg = cSelFocus
			}
			g.Fill(fx+1, gy+2+i, dw-2, 1, bg)
			mark := "○"
			if i == c.from {
				mark = "●"
			}
			g.Put(fx+2, gy+2+i, mark, stbg(cAccent, bg), 0)
			g.Put(fx+4, gy+2+i, ac.Cfg.Name, stbgb(cFG, bg, i == c.from), 8)
			fg := cDim
			if i == c.from {
				fg = cFG
			}
			g.Put(fx+13, gy+2+i, ac.Cfg.Email, stbg(fg, bg), dw-15)
			m.hits.add(hit{x: fx + 1, y: gy + 2 + i, w: dw - 2, h: 1, kind: hitFromAcct, idx: i})
		}
	}
	if c.picker != nil {
		m.hits.add(hit{w: g.W, h: g.H, kind: hitModal})
		c.picker.draw(g)
		c.picker.addHits(m, g)
		return
	}
	keybar(g, g.H-1, []keyHint{{"Ctrl+S", i18n.T("Gönder")}, {"Tab", "Alan"}, {"Ctrl+E", "Ek"}, {"Ctrl+K", i18n.T("Kimden")}, {"Esc", i18n.T("Kapat")}}, i18n.T("? Yardım"), &m.hits)
}

func firstAddr(l []*mail.Address) *mail.Address {
	if len(l) > 0 {
		return l[0]
	}
	return nil
}

func fmtWords(n int) string {
	return strings.TrimSpace(strings.Join([]string{itoaS(n), "kelime"}, " "))
}

func itoaS(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// editStyle: text field colours on a given background.
func editStyle(bg string) edit.Style {
	return edit.Style{FG: cFG, BG: bg, Faint: cFaint, Cursor: cAccent, SelBG: cTextSel}
}

func (m *Model) rememberSent(c *composer) {
	o, err := c.build(m, false)
	if err != nil || o == nil {
		return
	}
	b := m.recipBook()
	b.remember(append(append([]*mail.Address{}, o.To...), o.Cc...), m.opt.Now())
	_ = b.save()
}

func (c *composer) drawSuggest(m *Model, g *grid.Grid, x, fieldY, fw int) {
	if len(c.suggest) == 0 {
		return
	}
	w := min(56, max(24, fw))
	h := len(c.suggest) + 2
	y := fieldY + 1
	if y+h >= g.H-2 {
		y = max(3, fieldY-h)
	}
	g.Box(x, y, w, h, cBorderF, cRaised, "", "")
	for i, e := range c.suggest {
		bg := cRaised
		if i == c.suggestCur {
			bg = cSelFocus
		}
		yy := y + 1 + i
		g.Fill(x+1, yy, w-2, 1, bg)
		label := formatRecip(e)
		g.Put(x+2, yy, label, stbgb(cFG, bg, i == c.suggestCur), w-4)
		m.hits.add(hit{x: x + 1, y: yy, w: w - 2, h: 1, kind: hitSuggest, idx: i})
	}
}

// ---- attachment picker (Ctrl+E) ----
