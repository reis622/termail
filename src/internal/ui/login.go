package ui

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/oauth2"
	"os"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"termail/internal/edit"

	"termail/internal/account"
	"termail/internal/auth"
	"termail/internal/backend"
	"termail/internal/discover"
	"termail/internal/grid"
)

type loginModel struct {
	m         *Model
	canCancel bool
	email     edit.Editor
	relogin   int    // index of an existing account being signed in again, or -1
	passWarn  string // a hint shown with a refused sign-in (e.g. not 16 letters)
	pass      edit.Editor
	imapIn    edit.Editor
	smtpIn    edit.Editor
	focus     int
	manual    bool
	busy      bool
	status    string
	statusFG  string
	waiting   bool
	waitStart time.Time
	device    *auth.DeviceCode
	toDevice  func() // switches a waiting browser sign-in to the code sign-in
	authURL   string
	cancel    context.CancelFunc
	w         int
	forceMS   bool
	helpURL   string // shown on screen after Ctrl+O, in case no browser opens
}

// Login screen keys. They are shown as "Ctrl + O harfi" because in many
// console fonts the letter O looks like the digit 0.
const (
	keyAppPassword = "ctrl+o" // open the "how to get an app password" page
	keyCopyLink    = "ctrl+l" // copy the sign-in link (Microsoft)
)

type loginDoneMsg struct {
	acct       account.Account
	b          backend.Backend
	secretKind string
	secret     string
	err        error
	needManual bool
	msDetected bool
}

type loginDeviceMsg struct{ d *auth.DeviceCode }
type loginURLMsg struct{ url string }
type loginStatusMsg struct {
	text, fg string
}

// errUseCode cancels the browser wait when the user picks the code sign-in.
var errUseCode = errors.New("kodla giriş seçildi")

func newLogin(m *Model, canCancel bool) *loginModel {
	l := &loginModel{m: m, canCancel: canCancel, relogin: -1}
	l.email = edit.New(edit.Single)
	l.email.Placeholder = "ad@ornek.com"
	l.email.CharLimit = 200
	l.pass = edit.New(edit.Single)
	l.pass.Mask = '•'
	l.imapIn = edit.New(edit.Single)
	l.imapIn.Placeholder = "ör. mail.alanadi.com"
	l.smtpIn = edit.New(edit.Single)
	l.smtpIn.Placeholder = "ör. smtp.alanadi.com"
	l.email.Focus()
	return l
}

func (l *loginModel) resize(w int) { l.w = w }

func (l *loginModel) provider() (discover.Settings, bool) {
	return discover.Lookup(cleanAddress(l.email.Value()))
}

// keyShowPass shows / hides the password.
const keyShowPass = "f2"

// appPass: the provider wants an app password (Gmail, Yahoo, iCloud …).
func (l *loginModel) appPass() bool {
	s, ok := l.provider()
	return ok && s.Auth == discover.AuthAppPassword
}

// cleanPass is the password as sent: surrounding spaces and line breaks
// removed; app passwords lose their inner spaces too.
func (l *loginModel) cleanPass() string {
	return CleanSecret(l.pass.Value(), l.appPass())
}

// CleanSecret removes what can never be part of a password: NUL, control,
// zero-width and other invisible characters, surrounding spaces and line
// breaks; app passwords (shown as "abcd efgh ijkl mnop") lose every space.
// Letter case is never changed.
func CleanSecret(v string, app bool) string {
	var b strings.Builder
	for _, r := range v {
		switch {
		case !edit.Printable(r) && r != ' ' && r != '\t':
		case unicode.Is(unicode.Cf, r): // zero-width space / joiner, BOM, direction marks
		case app && unicode.IsSpace(r):
		case r == '\t':
			if !app {
				b.WriteByte(' ')
			}
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// gmailPassWarn: Gmail app passwords are 16 letters a–z.
func gmailPassWarn(p string) string {
	ok := len(p) == 16
	for _, r := range p {
		if r < 'a' || r > 'z' {
			ok = false
		}
	}
	if ok {
		return ""
	}
	return fmt.Sprintf("Gmail uygulama şifreleri 16 küçük harften oluşur; yazdığın şifre %d karakter.", len([]rune(p)))
}

// passSummary describes a password for the debug log without revealing it.
func passSummary(p string) string {
	var letters, digits, spaces, other int
	for _, r := range p {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		case unicode.IsSpace(r):
			spaces++
		default:
			other++
		}
	}
	return fmt.Sprintf("uzunluk %d (harf %d, rakam %d, boşluk %d, diğer %d)", len([]rune(p)), letters, digits, spaces, other)
}

// cleanAddress: an address as typed or pasted, without spaces, invisible
// or control characters, lower-cased.
func cleanAddress(v string) string {
	var b strings.Builder
	for _, r := range v {
		if edit.Printable(r) && !unicode.IsSpace(r) && r != 0x200b && r != 0x200c && r != 0x200d && r != 0x2060 && r != 0xfeff {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(strings.Trim(b.String(), "<>\"'"))
}

func (l *loginModel) isMS() bool {
	if l.forceMS {
		return true
	}
	s, ok := l.provider()
	return ok && s.Auth == discover.AuthMicrosoft
}

func (l *loginModel) inputs() []*edit.Editor {
	if l.isMS() {
		return []*edit.Editor{&l.email}
	}
	in := []*edit.Editor{&l.email, &l.pass}
	if l.manual {
		in = append(in, &l.imapIn, &l.smtpIn)
	}
	return in
}

func (l *loginModel) setFocus(i int) tea.Cmd {
	ins := l.inputs()
	if i < 0 {
		i = len(ins) - 1
	}
	if i >= len(ins) {
		i = 0
	}
	l.focus = i
	var cmd tea.Cmd
	for j, in := range ins {
		if j == i {
			cmd = in.Focus()
		} else {
			in.Blur()
		}
	}
	return cmd
}

func (l *loginModel) tick() tea.Cmd { return nil }

func (l *loginModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loginDeviceMsg:
		l.device = msg.d
		l.authURL = msg.d.URL
		l.status, l.statusFG = "", ""
		return nil
	case loginStatusMsg:
		l.status, l.statusFG = msg.text, msg.fg
		return nil
	case loginURLMsg:
		l.authURL = msg.url
		return nil
	case tea.KeyMsg:
		return l.key(msg)
	}
	return nil
}

// field is the focused sign-in field.
func (l *loginModel) field() *edit.Editor {
	ins := l.inputs()
	if l.focus >= len(ins) {
		l.focus = 0
	}
	return ins[l.focus]
}

func (l *loginModel) key(k tea.KeyMsg) tea.Cmd {
	m := l.m
	if l.busy {
		switch k.String() {
		case "k", "K":
			if l.device == nil && l.toDevice != nil {
				l.toDevice()
				l.toDevice = nil
				l.status, l.statusFG = "Kodla girişe geçiliyor…", cDim
			}
			return nil
		case "c", "C":
			if l.device != nil {
				copyTextFn(l.device.Code)
				l.status, l.statusFG = "Kod panoya kopyalandı.", cGreen
			}
			return nil
		}
	}
	switch k.String() {
	case "esc":
		if l.busy && l.cancel != nil {
			l.cancel()
			l.busy, l.waiting, l.device, l.toDevice = false, false, nil, nil
			l.status, l.statusFG = "Giriş iptal edildi.", cDim
			return nil
		}
		if l.canCancel {
			m.login = nil
			return nil
		}
		return m.exitNow()
	case "tab", "down":
		return l.setFocus(l.focus + 1)
	case "shift+tab", "up":
		return l.setFocus(l.focus - 1)
	case keyAppPassword:
		l.openAppPasswordHelp()
		return nil
	case keyCopyLink:
		if l.authURL != "" {
			copyTextFn(l.authURL)
			l.status, l.statusFG = "Bağlantı panoya kopyalandı; tarayıcının adres çubuğuna yapıştır.", cGreen
		}
		return nil
	case "enter":
		if l.busy {
			return nil
		}
		return l.submit()
	case keyShowPass:
		if l.pass.Mask == 0 {
			l.pass.Mask = '•'
		} else {
			l.pass.Mask = 0
		}
		return nil
	}
	if l.busy {
		return nil
	}
	_, cmd := m.editKey(l.field(), k)
	if l.field() == &l.pass && l.appPass() {
		// app passwords are shown with spaces ("abcd efgh ijkl mnop"); the
		// server wants the 16 letters only
		if v := l.pass.Value(); strings.ContainsAny(v, " \t\n") {
			l.pass.SetValue(strings.Join(strings.Fields(v), ""))
			l.pass.CursorEnd()
		}
	}
	if l.focus == 0 {
		l.forceMS = false
	}
	return cmd
}

// openAppPasswordHelp opens the provider's app-password page and always
// shows the address too, because the browser may not open.
func (l *loginModel) openAppPasswordHelp() {
	u := appPasswordURL(discover.Domain(l.email.Value()))
	l.helpURL = u
	copyTextFn(u)
	if err := openURLFn(u); err != nil {
		l.status, l.statusFG = "Tarayıcı açılamadı. Bu adresi tarayıcına yaz (panoya da kopyalandı):", cYellow
		return
	}
	l.status, l.statusFG = "Sayfa tarayıcında açılıyor. Açılmazsa bu adresi tarayıcına yaz:", cDim
}

func appPasswordURL(domain string) string {
	switch {
	case strings.Contains(domain, "yahoo"):
		return "https://login.yahoo.com/account/security/app-passwords"
	case domain == "icloud.com" || domain == "me.com" || domain == "mac.com":
		return "https://account.apple.com/account/manage"
	case strings.HasPrefix(domain, "yandex"):
		return "https://id.yandex.com/security/app-passwords"
	}
	return "https://myaccount.google.com/apppasswords"
}

func (l *loginModel) submit() tea.Cmd {
	l.helpURL = ""
	m := l.m
	email := cleanAddress(l.email.Value())
	if !auth.ValidAddress(email) || discover.Domain(email) == "" {
		l.status, l.statusFG = "Geçerli bir e-posta adresi yaz.", cRed
		return l.setFocus(0)
	}
	if email != l.email.Value() {
		l.email.SetValue(email)
		l.email.CursorEnd()
	}
	l.relogin = -1
	for i, a := range m.accts {
		if strings.EqualFold(a.Cfg.Email, email) {
			if a.State != stNeedsLogin && a.State != stOffline {
				l.status, l.statusFG = "Bu hesap zaten ekli.", cYellow
				return nil
			}
			l.relogin = i // signing in again: the saved password is replaced
		}
	}
	if l.isMS() {
		return l.startMicrosoft()
	}
	if l.focus == 0 && l.cleanPass() == "" {
		return l.setFocus(1)
	}
	if l.cleanPass() == "" {
		l.status, l.statusFG = "Şifreni yaz.", cRed
		return l.setFocus(1)
	}
	var manual *discover.Settings
	if l.manual {
		in, err1 := discover.ParseManual(l.imapIn.Value(), true)
		out, err2 := discover.ParseManual(l.smtpIn.Value(), false)
		if err1 != nil || err2 != nil {
			l.status, l.statusFG = "Gelen ve giden sunucu adlarını yaz.", cRed
			return nil
		}
		manual = &discover.Settings{Provider: "Diğer", Incoming: in, Outgoing: out, Auth: discover.AuthPassword, SaveSent: true}
	}
	l.busy = true
	l.status, l.statusFG = "Sunucu aranıyor…", cAccent
	ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
	l.cancel = cancel
	pass := l.cleanPass()
	l.passWarn = ""
	if s, ok := l.provider(); ok && s.Provider == "Gmail" {
		l.passWarn = gmailPassWarn(pass)
	}
	if m.opt.Timing || os.Getenv("TERMAIL_DEBUG") == "1" {
		m.log.Printf("giriş denemesi %s: şifre %s", email, passSummary(pass)) // never the password itself
	}
	demo := m.opt.Demo
	name := ""
	if m.opt.Config != nil {
		name = ""
	}
	return func() tea.Msg {
		defer cancel()
		var s discover.Settings
		if manual != nil {
			s = *manual
		} else if demo {
			s, _ = discover.Lookup(email)
			if s.Provider == "" {
				s = discover.Settings{Provider: "Diğer", Auth: discover.AuthPassword}
			}
		} else {
			var err error
			s, err = discover.New().Discover(ctx, email)
			if errors.Is(err, discover.ErrNotFound) {
				return loginDoneMsg{needManual: true}
			}
			if err != nil {
				return loginDoneMsg{err: err}
			}
			if s.Auth == discover.AuthMicrosoft {
				return loginDoneMsg{msDetected: true}
			}
		}
		acct := account.Account{Name: name, Email: email, Settings: s}
		if demo {
			return loginDoneMsg{acct: acct, b: demoBackendFor(email)}
		}
		b := backend.NewIMAP(backend.Config{Incoming: s.Incoming, Outgoing: s.Outgoing, Username: email, Password: pass})
		if err := b.Connect(ctx); err != nil {
			return loginDoneMsg{acct: acct, err: err}
		}
		return loginDoneMsg{acct: acct, b: b, secretKind: account.SecretPassword, secret: pass}
	}
}

// msUnavailable is shown only if a build has no Microsoft application ID.
const msUnavailable = "Microsoft ile giriş bu sürümde kullanılamıyor."

func (l *loginModel) msEnabled() bool {
	id := ""
	if l.m.opt.Config != nil {
		id = l.m.opt.Config.MSClientID
	}
	return auth.MSEnabled(id)
}

// startMicrosoft signs in through the browser (the page comes back to this
// computer by itself). If the browser can't be used — no browser, Microsoft
// refuses the return, or the user presses k — it switches to a short code
// the user types on any device.
func (l *loginModel) startMicrosoft() tea.Cmd {
	const p = auth.Microsoft
	m := l.m
	email := cleanAddress(l.email.Value())
	if !auth.ValidAddress(email) || discover.Domain(email) == "" {
		l.status, l.statusFG = "Önce e-posta adresini yaz.", cRed
		return l.setFocus(0)
	}
	if !l.msEnabled() {
		l.status, l.statusFG = msUnavailable, cYellow
		return nil
	}
	if m.opt.Demo { // demo: pretend the browser approval takes a few seconds
		l.busy, l.waiting, l.waitStart = true, true, m.opt.Now()
		l.status = ""
		l.authURL = auth.Authority + "/oauth2/v2.0/authorize?client_id=…"
		s, _ := discover.Lookup(email)
		acct := account.Account{Email: email, Settings: s}
		return tea.Tick(6*time.Second, func(time.Time) tea.Msg {
			return loginDoneMsg{acct: acct, b: demoBackendFor(email)}
		})
	}
	oc, err := oauthConfig(m.opt.Config, p, email)
	if err != nil {
		l.status, l.statusFG = msUnavailable, cYellow
		return nil
	}
	l.busy, l.waiting, l.waitStart = true, true, m.opt.Now()
	l.status = ""
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	l.cancel = cancel
	bctx, useCode := context.WithCancelCause(ctx)
	l.toDevice = func() { useCode(errUseCode) }
	send := m.send
	return func() tea.Msg {
		defer cancel()
		open := func(u string) error {
			send(loginURLMsg{url: u})
			if err := openURLFn(u); err != nil {
				send(loginStatusMsg{"Tarayıcı açılamadı. Ctrl+L ile bağlantıyı kopyalayıp tarayıcına yapıştır ya da k ile kodla gir.", cYellow})
				return err
			}
			return nil
		}
		tok, err := auth.BrowserLogin(bctx, oc, email, open)
		if err != nil && ctx.Err() == nil {
			msg, tryDevice := auth.Friendly(err)
			if !errors.Is(context.Cause(bctx), errUseCode) && !tryDevice {
				return loginDoneMsg{err: err}
			}
			if msg != "" {
				send(loginStatusMsg{msg, cYellow})
			}
			d, derr := auth.StartDevice(ctx, oc)
			if derr != nil {
				return loginDoneMsg{err: derr}
			}
			send(loginDeviceMsg{d: d})
			_ = openURLFn(d.URL)
			tok, err = auth.WaitDevice(ctx, oc, d)
		}
		if err != nil {
			return loginDoneMsg{err: err}
		}
		return finishMicrosoft(ctx, oc, email, tok)
	}
}

// finishMicrosoft checks the mailbox with the new sign-in and returns a
// backend that renews its access by itself.
func finishMicrosoft(ctx context.Context, oc *oauth2.Config, email string, tok *oauth2.Token) tea.Msg {
	if real := auth.IDTokenEmail(tok); real != "" && !strings.EqualFold(real, email) {
		email = real // the account actually chosen in the browser
	}
	s, ok := discover.Lookup(email)
	if !ok || s.Auth != discover.AuthMicrosoft {
		s, _ = discover.Lookup("x@outlook.com")
		s.Provider = "Microsoft 365"
	}
	acct := account.Account{Email: email, Settings: s}
	access := tok.AccessToken
	b := backend.NewIMAP(backend.Config{Incoming: s.Incoming, Outgoing: s.Outgoing, Username: email,
		Token: func(context.Context) (string, error) { return access, nil }})
	if err := b.Connect(ctx); err != nil {
		return loginDoneMsg{acct: acct, err: err}
	}
	b.Close()
	if tok.RefreshToken == "" {
		return loginDoneMsg{acct: acct, err: errors.New("no refresh token")}
	}
	seed := &oauth2.Token{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, Expiry: tok.Expiry, TokenType: tok.TokenType}
	ts := auth.TokenSourceFrom(oc, seed, func(n string) { _ = account.SetSecret(email, account.SecretRefresh, n) })
	rb := backend.NewIMAP(backend.Config{Incoming: s.Incoming, Outgoing: s.Outgoing, Username: email,
		Token: func(context.Context) (string, error) {
			t, err := ts.Token()
			if err != nil {
				return "", err
			}
			return t.AccessToken, nil
		}})
	return loginDoneMsg{acct: acct, b: rb, secretKind: account.SecretRefresh, secret: tok.RefreshToken}
}

func (m *Model) onLoginDone(d loginDoneMsg) tea.Cmd {
	l := m.login
	if l == nil {
		if d.b != nil {
			d.b.Close()
		}
		return nil
	}
	l.busy, l.waiting, l.device, l.toDevice = false, false, nil, nil
	switch {
	case d.needManual:
		l.manual = true
		l.status, l.statusFG = "Sunucu bulunamadı. Gelen ve giden sunucu adlarını yazabilirsin.", cYellow
		return l.setFocus(2)
	case d.msDetected:
		l.forceMS = true
		l.status, l.statusFG = "Bu bir Microsoft hesabı; Enter ile tarayıcıda giriş yap.", cAccent
		return l.setFocus(0)
	case d.err != nil:
		m.logErr("login", d.err)
		msg := friendly(d.err)
		if errors.Is(d.err, backend.ErrAuth) && l.isMS() {
			msg = "Microsoft girişi tamam ama posta kutusu açılmadı. Outlook ayarlarında posta programı erişiminin açık olduğundan emin ol."
		} else if errors.Is(d.err, backend.ErrAuth) {
			msg = backend.AuthReason(d.err)
			if l.passWarn != "" {
				msg = l.passWarn + " " + msg
			}
			if m.opt.Timing || os.Getenv("TERMAIL_DEBUG") == "1" {
				m.log.Printf("giriş reddedildi: %v", d.err) // server text only
			}
		}
		l.status, l.statusFG = msg, cRed
		return nil
	}
	cfg := m.opt.Config
	if cfg == nil {
		cfg = &account.Config{}
		m.opt.Config = cfg
	}
	if d.acct.Name == "" {
		d.acct.Name = cfg.DefaultName(d.acct.Settings.Provider)
	}
	if !m.opt.Demo {
		if d.secret != "" {
			if err := account.SetSecret(d.acct.Email, d.secretKind, d.secret); err != nil {
				m.logErr("keyring", err)
				m.toastInfo(1, "", "Giriş bilgisi Windows'ta saklanamadı; bir dahaki açılışta yeniden sorulacak.")
			}
		}
		if l.relogin < 0 {
			cfg.Accounts = append(cfg.Accounts, d.acct)
			if err := cfg.Save(); err != nil {
				m.toastErr("config", err)
			}
		}
	}
	if i := l.relogin; i >= 0 && i < len(m.accts) && strings.EqualFold(m.accts[i].Cfg.Email, d.acct.Email) {
		// signed in again: the new password (already saved over the old
		// one above) replaces the old connection
		a := m.accts[i]
		if old := a.B; old != nil {
			go old.Close()
		}
		a.B, a.Err, a.State = d.b, "", stConnecting
		m.cur, m.folder, m.cursor, m.offset = i, a.inbox(), 0, 0
		m.focus, m.login, m.active = focusList, nil, 0
		m.toastInfo(2, "", "Giriş bilgisi güncellendi: "+d.acct.Email)
		return m.connectAccount(i)
	}
	a := &Acct{Cfg: d.acct, B: d.b, Lists: map[string][]backend.Envelope{}, Loaded: map[string]bool{}, Open: true}
	m.accts = append(m.accts, a)
	i := len(m.accts) - 1
	m.cur, m.folder, m.cursor, m.offset = i, "INBOX", 0, 0
	m.focus = focusList
	m.login = nil
	m.active = 0
	m.toastInfo(2, "", "Hoş geldin! "+d.acct.Email+" eklendi.")
	return m.startAccount(i)
}

// ---- drawing ----

func (l *loginModel) draw(g *grid.Grid) {
	ms := l.isMS()
	cw, ch := min(64, g.W-4), 20
	if l.manual {
		ch += 5
	}
	if l.device != nil {
		ch += 6
	} else if l.waiting {
		ch += 2
	}
	if l.helpURL != "" {
		ch += 3
	}
	cx, cy := (g.W-cw)/2, max(3, (g.H-ch)/2-1)
	g.Put((g.W-11)/2, cy-3, " ◆ Termail ", stbgb(cBG, cAccent, true), 0)
	g.Box(cx, cy, cw, ch, cBorder, cPanel, "", "")
	fx, fw := cx+4, cw-8
	y := cy + 2
	g.Put(fx, y, "Hoş geldin", stb(cFG), 0)
	y++
	if ms {
		g.Put(fx, y, "E-posta adresini gir, yeter.", st(cDim), 0)
	} else {
		g.Put(fx, y, "E-posta adresini ve şifreni gir, yeter.", st(cDim), 0)
	}
	y += 2
	field := func(label string, in *edit.Editor, idx int, bg string) {
		focus := l.focus == idx
		lc := cDim
		if focus {
			lc = cAccent
		}
		g.Put(fx, y, label, grid.Style{FG: lc, Bold: focus}, 0)
		y++
		fbg := bg
		if focus {
			fbg = cRaised
		}
		g.Fill(fx, y, fw, 1, fbg)
		in.Draw(g, fx+1, y, fw-14, 1, editStyle(fbg))
		if idx == 0 {
			if s, ok := l.provider(); ok {
				name := s.Provider
				g.RPut(fx+fw-1, y, name+" ✓", stbg(cGreen, fbg))
			} else if l.forceMS {
				g.RPut(fx+fw-1, y, "Microsoft ✓", stbg(cGreen, fbg))
			}
		}
		y += 2
	}
	field("E-posta", &l.email, 0, cBG)
	if !ms {
		field("Şifre", &l.pass, 1, cRaised)
		if l.manual {
			field("Gelen posta sunucusu", &l.imapIn, 2, cBG)
			y--
			field("Giden posta sunucusu", &l.smtpIn, 3, cBG)
			y--
		}
		if s, ok := l.provider(); ok && s.Auth == discover.AuthAppPassword {
			g.Put(fx, y, "i", stb(cYellow), 0)
			g.Put(fx+2, y, s.Provider+"'de normal şifre yerine uygulama şifresi gir.", st(cDim), fw-2)
			y++
			g.Put(fx+2, y, "Nasıl alınır?  Ctrl + O harfi", st(cAccent), fw-2)
			y += 2
		} else {
			y++
		}
		g.Put(fx, y, "■", st(cGreen), 0)
		g.Put(fx+2, y, "Şifren Windows'ta güvenle saklanır, bir daha sorulmaz.", st(cFaint), fw-2)
		y += 2
	} else {
		g.Put(fx, y, "Microsoft hesabı için şifre gerekmez.", st(cFG), 0)
		y++
		g.Put(fx, y, "Tarayıcıda bir kez onay vermen yeterli.", st(cDim), 0)
		y += 2
		if !l.msEnabled() && l.status == "" {
			g.Put(fx, y, msUnavailable, st(cYellow), fw)
			y += 2
		}
		if l.waiting || l.device != nil {
			bh := 5
			if l.device != nil {
				bh = 9
			}
			g.Box(fx, y, fw, bh, cWaitBd, "", "", "")
			g.Fill(fx+1, y+1, fw-2, bh-2, cWaitBG)
			el := l.m.opt.Now().Sub(l.waitStart)
			if l.device != nil {
				g.Put(fx+2, y+1, "Tarayıcıda şu adrese git:", stbg(cDim, cWaitBG), 0)
				g.Put(fx+2, y+2, l.device.URL, stbgb(cAccent, cWaitBG, true), fw-4)
				g.Put(fx+2, y+4, "ve bu kodu gir:", stbg(cDim, cWaitBG), 0)
				big := " " + spaced(l.device.Code) + " "
				bx := fx + (fw-grid.Width(big))/2
				g.Fill(bx, y+5, grid.Width(big), 1, cAccent)
				g.Put(bx, y+5, big, stbgb(cBG, cAccent, true), fw-4)
				g.Put(fx+2, y+7, "c: kodu kopyala", stbg(cFaint, cWaitBG), fw-4)
			} else {
				g.Put(fx+2, y+1, "●", stbg(cAccent, cWaitBG), 0)
				g.Put(fx+4, y+1, "Tarayıcıda onay bekleniyor…", stbg(cFG, cWaitBG), 0)
				g.RPut(fx+fw-2, y+1, fmt.Sprintf("%d:%02d", int(el.Minutes()), int(el.Seconds())%60), stbg(cFaint, cWaitBG))
				g.Put(fx+2, y+2, "Açılmadıysa: Ctrl+L bağlantıyı kopyalar.", stbg(cDim, cWaitBG), fw-4)
				g.Put(fx+2, y+3, "Tarayıcıda hata mı var? k: kodla giriş", stbg(cDim, cWaitBG), fw-4)
			}
			y += bh + 1
		} else {
			y += 1
		}
		g.Put(fx, y, "■", st(cGreen), 0)
		g.Put(fx+2, y, "Giriş Windows'ta güvenle saklanır, bir daha sorulmaz.", st(cFaint), fw-2)
		y += 2
	}
	if l.status != "" {
		for _, line := range wrapText(l.status, fw) {
			g.Put(fx, y, line, st(l.statusFG), fw)
			y++
		}
		if l.helpURL != "" && !ms {
			g.Put(fx, y, l.helpURL, stb(cAccent), fw)
			y++
		}
		y++
	} else if l.busy && !l.waiting {
		y++
	}
	bt := "  Enter  ·  Giriş yap  "
	if ms {
		bt = "  Enter  ·  Tarayıcıda Microsoft ile giriş yap  "
	}
	if l.busy && !ms {
		bt = "  Giriş yapılıyor…  "
	}
	if ms && (!l.msEnabled() || l.busy) {
		bt = "" // nothing to press now
	}
	if bt != "" && y < cy+ch-1 {
		g.Put(cx+(cw-grid.Width(bt))/2, min(y, cy+ch-2), bt, stbgb(cBG, cAccent, true), 0)
	}
	note := "Sunucu ayarları otomatik bulunur; elle girmen gerekmez."
	g.Put((g.W-grid.Width(note))/2, cy+ch+1, note, st(cFaint), 0)
	esc := "Çıkış"
	if l.canCancel {
		esc = "Geri"
	}
	if l.busy {
		esc = "İptal"
	}
	if ms && !l.msEnabled() {
		keybar(g, g.H-1, []keyHint{{"Esc", esc}}, "", nil)
	} else if ms && l.device != nil {
		keybar(g, g.H-1, []keyHint{{"c", "Kodu kopyala"}, {"Ctrl+L", "Adresi kopyala"}, {"Esc", esc}}, "", nil)
	} else if ms && l.busy {
		keybar(g, g.H-1, []keyHint{{"Ctrl+L", "Bağlantıyı kopyala"}, {"k", "Kodla giriş"}, {"Esc", esc}}, "", nil)
	} else if ms {
		keybar(g, g.H-1, []keyHint{{"Enter", "Tarayıcıda giriş yap"}, {"Esc", esc}}, "", nil)
	} else {
		keybar(g, g.H-1, []keyHint{{"Enter", "Giriş"}, {"Tab", "Sonraki alan"}, {"F2", "Şifreyi göster/gizle"}, {"Ctrl + O harfi", "Uygulama şifresi nasıl alınır"}, {"Esc", esc}}, "", nil)
	}
}

// spaced shows a sign-in code with gaps so it is easy to read and type.
func spaced(code string) string {
	r := []rune(code)
	out := make([]string, len(r))
	for i, c := range r {
		out[i] = string(c)
	}
	return strings.Join(out, " ")
}
