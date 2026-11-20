// Package ui is the Bubble Tea user interface of Termail.
package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"termail/internal/auth"
	"termail/internal/grid"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/blocklist"
	"termail/internal/bodycache"
	"termail/internal/i18n"
	"termail/internal/localstore"
	"termail/internal/mailmsg"
	"termail/internal/opqueue"
	"termail/internal/toast"
	"termail/internal/undo"
)

const listBatch = 250 // envelopes fetched per background page, newest first

// Acct is one account as the UI sees it.
type Acct struct {
	Cfg     account.Account
	B       backend.Backend
	Folders []backend.Folder
	Open    bool
	Lists   map[string][]backend.Envelope
	Loaded  map[string]bool
	Saved   []backend.Envelope // flagged mail across folders (Folder set)
	SavedOK bool
	State   int // 0 connecting, 1 online, 2 offline, 3 needs sign-in
	Err     string

	// Build creates the backend (reads the saved sign-in from the Windows
	// Credential Manager); it runs in the background after the first frame.
	Build func() (backend.Backend, error)

	inboxMax     uint32
	inboxPrimed  bool
	cancel       context.CancelFunc
	inboxAsked   bool // the first inbox page was requested
	fullAsked    bool // the whole inbox list was requested
	connected    bool // the folder list arrived once
	fromCache    bool // the inbox list shown is the cached one of the last run
	syncGen      int  // increments each time a new folder load starts
	expectUnseen map[string]int
	expectSeen   map[string]int
	expectAll    map[string]int
	paging       bool // later pages of the current folder are still coming
	pageFolder   string
	pageKind     string
	pageOff      map[string]int // next completed offset per folder+"\x00"+kind
	pageQueued   map[string]int // next offset already requested (prefetch)
	needRefresh  bool           // Watch saw a change while paging
	folderSync   map[string]backend.FolderSync
}

const (
	stConnecting = iota
	stOnline
	stOffline
	stNeedsLogin
)

func (a *Acct) folderByRole(r backend.Role) string {
	for _, f := range a.Folders {
		if f.Role == r {
			return f.Name
		}
	}
	return ""
}

func (a *Acct) folder(name string) *backend.Folder {
	for i := range a.Folders {
		if a.Folders[i].Name == name {
			return &a.Folders[i]
		}
	}
	return nil
}

func (a *Acct) inbox() string {
	if n := a.folderByRole(backend.RoleInbox); n != "" {
		return n
	}
	return "INBOX"
}

// Kind is shown under the account name ("Gmail", "Outlook", "Diğer").
func (a *Acct) Kind() string {
	if a.Cfg.Settings.Provider != "" {
		return i18n.T(a.Cfg.Settings.Provider)
	}
	return i18n.T("Diğer")
}

type bodyKey struct {
	acct   int
	folder string
	uid    uint32
}

type focus int

const (
	focusList focus = iota
	focusFolders
	focusReader // the reading pane next to the list
)

// Timing of the list preview.
const (
	previewDelay = 150 * time.Millisecond // body fetch waits until the cursor settles
	bodyCacheCap = 60
)

type tabKind int

const (
	tabList tabKind = iota
	tabRead
	tabCompose
)

type tab struct {
	kind    tabKind
	title   string
	read    *reader
	compose *composer
}

// Options configure the program.
type Options struct {
	Config    *account.Config
	Accounts  []*Acct
	Demo      bool
	Blocks    *blocklist.List
	ShowLogin bool
	// OtherInstance: another running Termail holds the single-instance lock.
	OtherInstance bool
	// OnExit runs right before a fast exit that skips main's defers.
	OnExit func()
	// AfterStart lets demo mode inject events after the UI is running.
	AfterStart func(send func(tea.Msg))
	Now        func() time.Time
	Logger     *log.Logger
	// Connect builds a backend for an account (nil in demo mode).
	Connect func(a account.Account) (backend.Backend, int, error)
	// Store keeps the local cache, pending changes and unsent messages
	// (nil: nothing is kept, e.g. demo mode and most tests).
	Store *localstore.Store
	// Timing logs start-up / exit timings (--zaman, TERMAIL_TIMING=1).
	Timing bool
	Start  time.Time // process start (for the timings)
	// Lang, if set, is applied when the model is created ("tr" or "en").
	Lang i18n.Lang
}

// Model is the root Bubble Tea model.
type Model struct {
	opt            Options
	w, h           int
	realW          int // real window size (0 = not known yet)
	realH          int
	frames         int
	ops            *opqueue.Queue
	opData         map[int]any // op id -> payload
	undoOps        map[int]int // undo id -> queued op id
	opsTimer       bool
	emptying       *emptyJob
	inView         bool
	visMemo        []backend.Envelope
	wrapC          wrapCache
	gridBuf        *grid.Grid
	sendsPending   int
	rowCache       map[rowKey]rowText
	pendingF       *pendingFolder // folder being previewed, list not loaded yet
	previewSeq     int
	listReq        int
	accts          []*Acct
	cur            int
	folder         string
	tabs           []*tab
	active         int
	focus          focus
	hoverOn        bool
	hover          hit
	hoverX, hoverY int
	wantBrowser    *browserReq
	fcursor        int
	cursor         int
	offset         int
	bodies         *bodycache.LRU[bodyKey, *mailmsg.Parsed]
	pv             *reader  // state of the reading pane (follows the highlighted mail)
	selSeq         int      // bumped whenever the highlighted mail changes
	virt           virtView // a merged view instead of one folder
	acctPicker     *accountPicker
	loading        map[bodyKey]bool
	bodyErr        map[bodyKey]string
	toasts         toast.Manager
	recip          *recipBook
	undo           undo.Stack
	undoToasts     map[int]int // undo id -> toast id
	help           bool
	search         *searchBox
	newFolder      *newFolderBox
	ctxMenu        *ctxMenu
	bodyFind       *bodyFind
	login          *loginModel
	confirm        *confirmBox
	picker         *folderPicker
	links          *linkPicker // the "Bağlantılar" list
	hits           hitMap      // clickable places of the last frame
	noRender       bool        // the last message changed nothing on screen
	lastOut        string      // the last frame (returned again when noRender)
	lastClk        lastClick
	lastPaste      lastPaste
	pasteEcho      []rune    // after our own Ctrl+V: the same text replayed as keys is dropped
	echoUntil      time.Time // ... while this lasts
	typedRecent    []rune    // keys typed in a quick burst (a terminal's key-by-key paste)
	typedAt        time.Time
	tv             textView // the mail text on screen (for mouse selection)
	rsel           *textSel // selected text in the reading pane
	clickN         clickRun
	fdrag          fieldDrag
	freeScroll     bool // the wheel moved the list; the cursor may be off screen
	mouse          bool // mouse enabled (config "fare")
	blocks         *blockScreen
	events         chan tea.Msg
	profile        termenv.Profile
	quitting       bool
	ctx            context.Context
	cancel         context.CancelFunc
	log            *log.Logger
	prog           *tea.Program
	replay         []localstore.Op // changes to send once their account is up
	inflight       map[*composer]bool
	quitWait       bool // q pressed while sending: leave when it is done
	persisted      bool
	exitAt         time.Time
	bg             sync.WaitGroup // background cache writes
	marked         map[string]bool
	prefs          localstore.Prefs // remembered view settings (prefs.go)
	prefsMu        sync.Mutex
	grouped        bool                      // list grouped by sender (Shift+G)
	grpOpen        map[string]bool           // expanded sender groups
	grps           map[string]*grpInfo       // groups of the last grouped list
	grpChild       []bool                    // row i is a mail inside an expanded group
	stickySec      map[foldKey]bool          // mails toggled with i stay in their list (true = Gelen Kutusu) until the folder changes
	readView       bool                      // Okunanlar: the read half of the inbox (account inbox or Tüm Gelen Kutuları)
	opened         *foldKey                  // the mail opened with Enter: stays in Gelen Kutusu while the reading pane shows it
	bulk           *bulkDel                  // the last Shift+D move to the trash (for u)
	flagMarks      map[flagMarkKey]*flagMark // local saved / read changes that win over older snapshots
	brandCache     map[string]string         // address -> group key
	foldCache      map[foldKey]foldVal       // search: folded header text per mail
	bodyFold       map[*mailmsg.Parsed]string
}

// New creates the model.
func New(opt Options) *Model {
	if opt.Lang != "" {
		i18n.Set(opt.Lang)
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Start.IsZero() {
		opt.Start = time.Now()
	}
	if opt.Blocks == nil {
		opt.Blocks, _ = blocklist.Load("")
	}
	if opt.Logger == nil {
		opt.Logger = log.New(ioDiscard{}, "", 0)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{opt: opt, accts: opt.Accounts, w: 80, h: 24,
		tabs:    []*tab{{kind: tabList}},
		bodies:  bodycache.New[bodyKey, *mailmsg.Parsed](bodyCacheCap),
		loading: map[bodyKey]bool{}, bodyErr: map[bodyKey]string{},
		ops: opqueue.New(opsWindow), opData: map[int]any{}, undoOps: map[int]int{},
		events:  make(chan tea.Msg, 256),
		profile: termenv.EnvColorProfile(),
		ctx:     ctx, cancel: cancel, log: opt.Logger,
		inflight: map[*composer]bool{}, marked: map[string]bool{},
	}
	m.bodies.Pinned = m.bodyPinned
	if m.profile == termenv.Ascii {
		m.profile = termenv.ANSI256
	}
	if os.Getenv("TERMAIL_TRUECOLOR") == "1" || os.Getenv("WT_SESSION") != "" {
		m.profile = termenv.TrueColor // Windows Terminal always supports 24-bit colour
	}
	for i, a := range m.accts {
		a.Open = i == 0
		if a.Lists == nil {
			a.Lists = map[string][]backend.Envelope{}
			a.Loaded = map[string]bool{}
		}
	}
	m.folder = "INBOX"
	m.grpOpen = map[string]bool{}
	m.loadPrefs()
	if len(m.accts) == 0 || opt.ShowLogin {
		m.login = newLogin(m, len(m.accts) > 0)
	}
	return m
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

// SetProfile forces a colour profile (tests / screenshots).
func (m *Model) SetProfile(p termenv.Profile) { m.profile = p }

// ---- messages ----

type tickMsg time.Time
type foldersMsg struct {
	acct    int
	folders []backend.Folder
	err     error
}
type listMsg struct {
	acct   int
	folder string
	envs   []backend.Envelope
	err    error
	check  bool      // came from a new-mail notification
	page   bool      // a window of the folder; more may follow
	req    int       // folder-preview request id (0 = ordinary load)
	asked  time.Time // when the server was asked (older than a local flag change: the local one wins)
	offset int
	total  int    // server total for this query (mailbox or unseen/seen)
	kind   string // "", "unseen", "seen"
	gen    int    // Acct.syncGen when this request was started
}
type bodyMsg struct {
	key bodyKey
	p   *mailmsg.Parsed
	err error
}
type watchMsg struct {
	acct int
	ev   backend.Event
}
type moveDoneMsg struct {
	undoID int
	acct   int
	folder string
	env    backend.Envelope
	newUID uint32
	err    error
	what   string // "delete", "archive", "move", "block", "autoblock"
	dest   string
}
type undoDoneMsg struct {
	e      undo.Entry
	err    error
	newUID uint32
}
type flagDoneMsg struct {
	err    error
	has    bool // the fields below are set (optimistic change to roll back)
	acct   int
	folder string
	env    backend.Envelope
	flag   backend.Flag
	on     bool
}
type seenTimerMsg struct {
	key bodyKey
	seq int
}

// bodyLoadMsg fires when the cursor has rested on a mail for previewDelay.
type bodyLoadMsg struct {
	key bodyKey
	seq int
}
type savedMsg struct {
	path string
	err  error
}
type sendDoneMsg struct {
	c       *composer
	err     error
	draft   bool
	toastID int
}
type folderCreatedMsg struct {
	acct int
	name string
	then func() tea.Cmd
	err  error
}

// InjectMsg lets demo mode / tests push a message into the program.
type InjectMsg struct{ Fn func(m *Model) tea.Cmd }

func (m *Model) send(msg tea.Msg) {
	select {
	case m.events <- msg:
	default:
	}
}

func (m *Model) waitEvent() tea.Cmd {
	return func() tea.Msg { return <-m.events }
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init starts background work.
func (m *Model) Init() tea.Cmd {
	m.mark("Init")
	cmds := []tea.Cmd{tick(), m.waitEvent(), tea.SetWindowTitle("Termail"), m.loadLocal()}
	for i := range m.accts {
		cmds = append(cmds, m.startAccount(i))
	}
	if m.opt.AfterStart != nil {
		go m.opt.AfterStart(m.send)
	}
	if m.opt.OtherInstance {
		m.toasts.Push(toast.Info, "", i18n.T("Termail başka bir pencerede zaten açık."), m.opt.Now(), 6*time.Second)
	}
	return tea.Batch(cmds...)
}

func (m *Model) loadFolders(i int) tea.Cmd {
	b := m.accts[i].B
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
		defer cancel()
		fs, err := b.Folders(ctx)
		return foldersMsg{acct: i, folders: fs, err: err}
	}
}

func (m *Model) loadList(i int, folder string, check bool) tea.Cmd {
	return m.startListLoad(i, folder, check, 0)
}

func (m *Model) loadBody(k bodyKey) tea.Cmd {
	if m.bodies.Has(k) || m.loading[k] || k.acct >= len(m.accts) {
		return nil
	}
	if p := m.bodyFromDisk(k); p != nil {
		m.bodies.Put(k, p)
		return func() tea.Msg { return bodyMsg{key: k, p: p} }
	}
	if m.accts[k.acct].B == nil {
		return nil
	}
	m.loading[k] = true
	b := m.accts[k.acct].B
	st := m.opt.Store
	email := m.accts[k.acct].Cfg.Email
	folder, uid := k.folder, k.uid
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
		defer cancel()
		raw, err := b.FetchRaw(ctx, k.folder, k.uid)
		if err != nil {
			return bodyMsg{key: k, err: err}
		}
		if st != nil {
			_ = st.SaveBody(email, folder, uid, raw)
		}
		p, err := mailmsg.Parse(raw)
		return bodyMsg{key: k, p: p, err: err}
	}
}

func (m *Model) bodyFromDisk(k bodyKey) *mailmsg.Parsed {
	st := m.opt.Store
	if st == nil || k.acct >= len(m.accts) {
		return nil
	}
	raw := st.LoadBody(m.accts[k.acct].Cfg.Email, k.folder, k.uid)
	if len(raw) == 0 {
		return nil
	}
	p, err := mailmsg.Parse(raw)
	if err != nil {
		return nil
	}
	return p
}

// friendly turns an error into plain Turkish without technical words.
func friendly(err error) string {
	if err == nil {
		return ""
	}
	var ne net.Error
	switch {
	case authMsg(err) != "":
		return authMsg(err)
	case errors.Is(err, backend.ErrMsgGone):
		return i18n.T("İleti sunucuda bulunamadı; başka bir yerden silinmiş ya da taşınmış olabilir.")
	case errors.Is(err, backend.ErrFlagNotApplied):
		return i18n.T("Sunucu değişikliği kabul etmedi.")
	case errors.Is(err, mailmsg.ErrUnreadable):
		return i18n.T("Bu ileti görüntülenemedi.")
	case errors.Is(err, backend.ErrAuth):
		return backend.AuthReason(err)
	case errors.Is(err, context.DeadlineExceeded):
		return i18n.T("Sunucu zamanında yanıt vermedi.")
	case errors.Is(err, context.Canceled):
		return i18n.T("İşlem iptal edildi.")
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return i18n.T("Sunucu bağlantıyı beklenmedik şekilde kapattı.")
	case errors.As(err, &ne):
		return i18n.T("Sunucuya ulaşılamadı. İnternet bağlantını kontrol et.")
	}
	s := err.Error()
	if strings.Contains(s, "certificate") || strings.Contains(s, "x509") {
		return i18n.T("Sunucunun güvenlik sertifikası doğrulanamadı; bağlanılmadı.")
	}
	if strings.Contains(s, "no such host") || strings.Contains(s, "connection refused") {
		return i18n.T("Sunucuya ulaşılamadı.")
	}
	return i18n.T("Bir sorun oluştu, tekrar dene.")
}

func (m *Model) logErr(what string, err error) {
	if err != nil {
		m.log.Printf("%s: %v", what, err)
	}
}

func (m *Model) toastErr(what string, err error) {
	m.logErr(what, err)
	m.toasts.Push(toast.Error, "", friendly(err), m.opt.Now(), 6*time.Second)
}

func (m *Model) toastInfo(kind toast.Kind, label, text string) {
	m.toasts.Push(kind, label, text, m.opt.Now(), 0)
}

// ---- current list helpers ----

func (m *Model) acct() *Acct {
	if m.cur < len(m.accts) {
		return m.accts[m.cur]
	}
	return nil
}

// readTitle is the virtual folder of read inbox mail.
func readTitle() string { return i18n.T("Okunanlar") }

// inUnreadList tells which half of the inbox a mail belongs to: Gelen
// Kutusu (unread) or Okunanlar (read, no server move). A mail toggled with
// i stays put until the folder changes or is refreshed; the mail opened with
// Enter stays while the reading pane shows it, so the list does not jump.
func (m *Model) inUnreadList(ai int, fo string, e backend.Envelope) bool {
	k := foldKey{ai, fo, e.UID}
	if v, ok := m.stickySec[k]; ok {
		return v
	}
	// In Gelen Kutusu only: keep the opened mail in this list while the
	// reading pane shows it, so the row does not vanish and the cursor
	// does not land on the next mail. In Okunanlar the mail is already
	// read and must stay in this list; treating it as unread would drop
	// it and open the following row instead.
	if !m.readView && m.opened != nil && *m.opened == k && m.focus == focusReader && m.pv != nil && m.pv.key == (bodyKey{ai, fo, e.UID}) {
		return true
	}
	return !e.Seen
}

// splitRead keeps the Gelen Kutusu (unread) or the Okunanlar (read) half
// of an inbox list; merged lists carry their own account and folder.
func (m *Model) splitRead(l []backend.Envelope, ai int, fo string) []backend.Envelope {
	// Okunanlar was removed: Gelen Kutusu (and Tüm Gelen Kutuları) lists
	// every mail; read vs unread is only a typographic contrast.
	_ = ai
	_ = fo
	return l
}

func sortReadAt(l []backend.Envelope) {
	sort.SliceStable(l, func(i, j int) bool {
		ri, rj := l[i].ReadAt, l[j].ReadAt
		if !ri.Equal(rj) {
			return ri.After(rj)
		}
		if !l[i].Date.Equal(l[j].Date) {
			return l[i].Date.After(l[j].Date)
		}
		return l[i].UID > l[j].UID
	})
}

// inboxView reports whether the list is an inbox half (Gelen Kutusu or
// Okunanlar, one account or all of them).
func (m *Model) inboxView() bool {
	switch m.virt {
	case viewUnified:
		return true
	case viewFolder:
		a := m.acct()
		return a != nil && m.folder == a.inbox()
	}
	return false
}

// inTrashView reports whether the open list is a trash folder.
func (m *Model) inTrashView() bool {
	if m.virt != viewFolder {
		return false
	}
	a := m.acct()
	if a == nil {
		return false
	}
	if f := a.folder(m.folder); f != nil {
		return f.Role == backend.RoleTrash || backend.RoleByName(f.Name) == backend.RoleTrash
	}
	return backend.RoleByName(m.folder) == backend.RoleTrash
}

func (m *Model) visible() []backend.Envelope {
	if m.inView && m.visMemo != nil {
		return m.visMemo
	}
	v := m.computeVisible()
	if m.inView {
		m.visMemo = v
	}
	return v
}

func (m *Model) computeVisible() []backend.Envelope {
	if m.acct() == nil {
		return nil
	}
	if m.searching() {
		return m.searchResults()
	}
	l := m.baseList()
	if m.groupsOn() {
		return m.groupRows(l)
	}
	return l
}

func (m *Model) selected() (backend.Envelope, bool) {
	v := m.visible()
	if m.cursor >= 0 && m.cursor < len(v) && !isGroupRow(v[m.cursor]) {
		return v[m.cursor], true
	}
	return backend.Envelope{}, false
}

func (m *Model) clampCursor() {
	m.clampCursorTo(len(m.visible()))
}

// clampCursorTo keeps the cursor on a real row whenever the list is not
// empty, and keeps the scroll offset from starting past the last row
// (which would draw no highlight at all).
func (m *Model) clampCursorTo(n int) {
	if n <= 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.offset < 0 {
		m.offset = 0
	}
	if m.offset >= n {
		m.offset = m.cursor
	}
}

func (m *Model) removeEnv(ai int, folder string, uid uint32) {
	a := m.accts[ai]
	for i, x := range a.Saved {
		if x.Folder == folder && x.UID == uid {
			a.Saved = append(append([]backend.Envelope(nil), a.Saved[:i]...), a.Saved[i+1:]...)
			break
		}
	}
	l := a.Lists[folder]
	for i, e := range l {
		if e.UID == uid {
			a.Lists[folder] = append(append([]backend.Envelope(nil), l[:i]...), l[i+1:]...)
			if f := a.folder(folder); f != nil {
				f.Total--
				if !e.Seen && f.Unread > 0 {
					f.Unread--
				}
			}
			if !e.Seen && folder == a.inbox() && a.expectUnseen != nil && a.expectUnseen[folder] > 0 {
				a.expectUnseen[folder]--
			}
			break
		}
	}
	m.patchSearch(ai, folder, uid, func(*backend.Envelope) bool { return false })
	m.clampCursor()
}

func (m *Model) insertEnv(ai int, folder string, e backend.Envelope) {
	a := m.accts[ai]
	// A flagged mail moved to Trash/Spam must not reappear in Kaydedilenler
	// (that copy used a placeholder UID and looked like a failed delete).
	if e.Flagged && !folderIsGone(a, folder) {
		c := e
		c.Acct, c.Folder = ai, folder
		a.Saved = append(a.Saved, c)
	}
	l := append([]backend.Envelope{e}, a.Lists[folder]...)
	sortEnv(l)
	a.Lists[folder] = l
	if f := a.folder(folder); f != nil {
		f.Total++
		if !e.Seen {
			f.Unread++
		}
	}
	m.clampCursor()
}

func folderIsGone(a *Acct, folder string) bool {
	if a == nil {
		return false
	}
	if f := a.folder(folder); f != nil {
		return f.Role == backend.RoleTrash || f.Role == backend.RoleJunk
	}
	return backend.RoleByName(folder) == backend.RoleTrash || backend.RoleByName(folder) == backend.RoleJunk
}

func sortEnv(l []backend.Envelope) {
	for i := 1; i < len(l); i++ {
		for j := i; j > 0 && (l[j].Date.After(l[j-1].Date) || (l[j].Date.Equal(l[j-1].Date) && l[j].UID > l[j-1].UID)); j-- {
			l[j], l[j-1] = l[j-1], l[j]
		}
	}
}

func (m *Model) setSeenLocal(ai int, folder string, uid uint32, seen bool) bool {
	a := m.accts[ai]
	for i := range a.Saved {
		if a.Saved[i].Folder == folder && a.Saved[i].UID == uid {
			a.Saved[i].Seen = seen
			if seen {
				a.Saved[i].ReadAt = m.opt.Now()
			} else {
				a.Saved[i].ReadAt = time.Time{}
			}
		}
	}
	m.patchSearch(ai, folder, uid, func(e *backend.Envelope) bool { e.Seen = seen; return true })
	for i := range a.Lists[folder] {
		e := &a.Lists[folder][i]
		if e.UID == uid {
			if e.Seen == seen {
				return false
			}
			e.Seen = seen
			if seen {
				e.ReadAt = m.opt.Now()
			} else {
				e.ReadAt = time.Time{}
			}
			if f := a.folder(folder); f != nil {
				if seen && f.Unread > 0 {
					f.Unread--
				} else if !seen {
					f.Unread++
				}
			}
			if folder == a.inbox() {
				if seen {
					a.adjustExpect(folder, "unseen", -1)
					a.adjustExpect(folder, "seen", 1)
				} else {
					a.adjustExpect(folder, "unseen", 1)
					a.adjustExpect(folder, "seen", -1)
				}
			}
			m.clampCursor()
			return true
		}
	}
	m.clampCursor()
	return false
}

func (m *Model) setFlag(ai int, folder string, uid uint32, f backend.Flag, on bool) tea.Cmd {
	return m.queueFlag(ai, folder, backend.Envelope{UID: uid}, f, on)
}

// markRead marks a message read locally + on the server and removes it from
// the new-mail toast.
func (m *Model) markRead(ai int, folder string, uid uint32) tea.Cmd {
	m.toasts.MarkRead(ai, folder, uid)
	if m.setSeenLocal(ai, folder, uid, true) {
		return m.setFlag(ai, folder, uid, backend.FlagSeen, true)
	}
	return nil
}

// Run starts the program.
func Run(opt Options) error {
	m := New(opt)
	// Nothing may print to the terminal while the UI runs (it would scroll
	// the screen): the standard logger goes to our log (or nowhere).
	log.SetOutput(m.log.Writer())
	prepareConsole()
	if w, h, ok := termSizeFn(); ok {
		m.applySize(w, h)
	}
	popts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithFPS(60)}
	if m.opt.Config.MouseOn() {
		// All-motion reports pointer movement so hover highlights can update.
		// Bubble Tea switches mouse reporting off again when the program ends.
		m.mouse = true
		popts = append(popts, tea.WithMouseAllMotion())
	}
	return m.run(tea.NewProgram(m, popts...))
}

// run drives the program and then leaves fast (see lifecycle.go).
func (m *Model) run(p *tea.Program) error {
	m.prog = p
	stop := onConsoleClose(func() {
		m.consoleClosing(p, func() {
			if m.opt.OnExit != nil {
				m.opt.OnExit()
			}
			os.Exit(0)
		})
	})
	defer stop()
	m.mark("Run (tea.Program başlıyor)")
	_, err := p.Run()
	if m.quitting && errors.Is(err, tea.ErrProgramKilled) {
		err = nil // our own fast exit
	}
	m.cancel()
	m.persistForExit() // (already done for q / Ctrl+C; here for other ways out)
	took := closeBackends(m.accts, &m.bg, exitBudget)
	if m.opt.Timing {
		since := m.exitAt
		if since.IsZero() {
			since = time.Now().Add(-took)
		}
		m.log.Printf("zaman çıkış: tuş → terminal geri + bağlantılar kapandı %8.1f ms (bağlantılar %0.1f ms)",
			float64(time.Since(since).Microseconds())/1000, float64(took.Microseconds())/1000)
	}
	return err
}

func fmtErr(format string, a ...any) error { return fmt.Errorf(format, a...) }

// body returns a cached message body or nil.
func (m *Model) body(k bodyKey) *mailmsg.Parsed {
	p, _ := m.bodies.Get(k)
	return p
}

// bodyPinned keeps bodies that an open tab or the reading pane still shows.
func (m *Model) bodyPinned(k bodyKey) bool {
	if m.pv != nil && m.pv.key == k {
		return true
	}
	for _, t := range m.tabs {
		if t.read != nil && t.read.key == k {
			return true
		}
		if t.compose != nil && t.compose.orig != nil && t.compose.orig.key == k {
			return true
		}
	}
	return false
}

// authMsg is the plain Turkish text for a Microsoft sign-in error ("" if
// err is something else).
func authMsg(err error) string {
	msg, _ := auth.Friendly(err)
	return msg
}

// wrapCache keeps the wrapped lines of the body shown in the reading pane,
// so moving the cursor or a timer tick does not re-wrap a long mail. It is
// rebuilt when the mail, its text or the width changes.
type wrapCache struct {
	key   bodyKey
	text  string
	w     int
	lines []wline
	paras []string
}

func (m *Model) wrapped(k bodyKey, text string, w int) ([]wline, []string) {
	c := &m.wrapC
	if c.lines != nil && c.key == k && c.w == w && c.text == text {
		return c.lines, c.paras
	}
	lines, paras := wrapSpans(text, w)
	*c = wrapCache{key: k, text: text, w: w, lines: lines, paras: paras}
	return c.lines, c.paras
}

// Per-message list row text (cleaned + truncated), so drawing a row does no
// Unicode clean-up or width work after the first time. Keyed by the source
// strings and widths, so a changed subject or a resize simply misses.
type rowKey struct {
	acct   int
	uid    uint32
	from   string
	subj   string
	fw, sw int
}

type rowText struct{ from, subj string }

func (m *Model) rowText(e backend.Envelope, fw, sw int) rowText {
	k := rowKey{e.Acct, e.UID, e.From + "\x00" + e.FromAddr, e.Subject, fw, sw}
	if r, ok := m.rowCache[k]; ok {
		return r
	}
	if m.rowCache == nil || len(m.rowCache) > 4000 {
		m.rowCache = make(map[rowKey]rowText, 256)
	}
	from := tidy(e.From)
	if from == "" {
		from = e.FromAddr
	}
	r := rowText{from: grid.Truncate(from, fw), subj: grid.Truncate(trimSubj(e.Subject), sw)}
	m.rowCache[k] = r
	return r
}
