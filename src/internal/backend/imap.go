package backend

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"termail/internal/auth"
	"termail/internal/discover"
)

// Config for a real account.
type Config struct {
	Incoming, Outgoing discover.Server
	Username           string
	Password           string
	// Token returns a fresh access token (Microsoft sign-in). When
	// set, Password is ignored.
	Token func(ctx context.Context) (string, error)
}

// ErrAuth means the server rejected the credentials.
var ErrAuth = errors.New("giriş reddedildi")

// IMAP is the real backend.
type IMAP struct {
	cfg Config
	// main serves folder lists and STATUS. list is a dedicated connection
	// for header pages, so later inbox pages are not queued behind
	// Folders/Flagged on main. body is for message bodies; ops is for
	// changes (move, delete, flags); IDLE has its own connection (Watch).
	main, body, ops, list, pref *session
	// srch is used only for searches (see SearchCriteria).
	srch       *session
	cacheMu    sync.Mutex
	uidCache   uidPageCache
	syncMu     sync.Mutex
	folderSync map[string]FolderSync
	// smtp is a kept-open, signed-in outgoing connection (opened when a
	// compose tab opens, reused for sends, replaced when stale).
	smtpMu sync.Mutex
	smtpC  *smtp.Client
	smtpAt time.Time
	gmail  bool // server is Gmail (X-GM-EXT-1)

	// Every open IMAP connection (reading, changes, IDLE), so Close can
	// drop them all at once without taking any session lock.
	closed atomic.Bool
	liveMu sync.Mutex
	live   map[*imapclient.Client]struct{}
}

// ErrClosed means the backend was closed (the program is exiting).
var ErrClosed = errors.New("bağlantı kapatıldı")

func (b *IMAP) track(c *imapclient.Client) {
	b.liveMu.Lock()
	if b.live == nil {
		b.live = map[*imapclient.Client]struct{}{}
	}
	b.live[c] = struct{}{}
	b.liveMu.Unlock()
	go func() {
		<-c.Closed()
		b.liveMu.Lock()
		delete(b.live, c)
		b.liveMu.Unlock()
	}()
}

// session is one signed-in connection with its selected folder.
type session struct {
	b        *IMAP
	mu       sync.Mutex
	c        *imapclient.Client
	selected string
}

type uidPageCache struct {
	folder string
	mode   int // 0 all, 1 unseen, 2 seen
	uids   []uint32
}

// NewIMAP creates a backend; call Connect to sign in.
func NewIMAP(cfg Config) *IMAP {
	b := &IMAP{cfg: cfg}
	b.main, b.body, b.ops, b.srch, b.list, b.pref = &session{b: b}, &session{b: b}, &session{b: b}, &session{b: b}, &session{b: b}, &session{b: b}
	return b
}

func wordDecoder() *mime.WordDecoder { return &mime.WordDecoder{CharsetReader: charset.Reader} }

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (b *IMAP) dial(handler *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	if b.closed.Load() {
		return nil, ErrClosed
	}
	c, err := b.dialRaw(handler)
	if err != nil {
		return nil, err
	}
	b.track(c)
	if b.closed.Load() { // closed while we were dialling
		c.Close()
		return nil, ErrClosed
	}
	return c, nil
}

func (b *IMAP) dialRaw(handler *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	s := b.cfg.Incoming
	opts := &imapclient.Options{
		WordDecoder:           wordDecoder(),
		UnilateralDataHandler: handler,
		Dialer:                &net.Dialer{Timeout: 15 * time.Second},
		TLSConfig:             &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12},
		DebugWriter:           debugW(handler != nil),
	}
	switch s.Security {
	case discover.TLS:
		return imapclient.DialTLS(s.Addr(), opts)
	case discover.StartTLS:
		return imapclient.DialStartTLS(s.Addr(), opts)
	case discover.None:
		if !isLoopback(s.Host) {
			return nil, errors.New("güvensiz bağlantı reddedildi")
		}
		return imapclient.DialInsecure(s.Addr(), opts)
	}
	return nil, fmt.Errorf("bilinmeyen bağlantı türü %q", s.Security)
}

func (b *IMAP) login(ctx context.Context, c *imapclient.Client) error {
	if b.cfg.Token != nil {
		tok, err := b.cfg.Token(ctx)
		if err != nil {
			return err
		}
		if err := c.Authenticate(auth.NewXOAUTH2(b.cfg.Username, tok)); err != nil {
			return fmt.Errorf("%w: %v", ErrAuth, err)
		}
		return nil
	}
	user := strings.ToLower(strings.TrimSpace(b.cfg.Username))
	var err error
	if c.Caps().Has(imap.AuthCap(sasl.Plain)) {
		// AUTHENTICATE PLAIN: user and password go base64-encoded, so no
		// quoting / literal issues with unusual characters
		err = c.Authenticate(sasl.NewPlainClient("", user, b.cfg.Password))
	} else {
		err = c.Login(user, b.cfg.Password).Wait()
	}
	if err != nil {
		var ie *imap.Error
		if errors.As(err, &ie) {
			return &AuthError{Code: string(ie.Code), Text: ie.Text}
		}
		return err
	}
	return nil
}

func (s *session) connectLocked(ctx context.Context) error {
	b := s.b
	if s.c != nil {
		select {
		case <-s.c.Closed():
			s.c = nil
		default:
			return nil
		}
	}
	c, err := b.dial(nil)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err := b.login(ctx, c); err != nil {
		c.Close()
		return err
	}
	s.c = c
	s.selected = ""
	return nil
}

// Connect signs in (used to verify credentials at login time).
func (b *IMAP) Connect(ctx context.Context) error {
	s := b.main
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectLocked(ctx)
}

// do runs f with a connected client, reconnecting once on a dropped connection.
func (s *session) do(ctx context.Context, f func(c *imapclient.Client) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doLocked(ctx, f)
}

func (s *session) doLocked(ctx context.Context, f func(c *imapclient.Client) error) error {
	if err := ctx.Err(); err != nil {
		return err // given up while waiting for the connection
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := s.connectLocked(ctx); err != nil {
			return err
		}
		c := s.c
		stop := context.AfterFunc(ctx, func() { c.Close() })
		err := f(c)
		stop()
		if err == nil {
			return nil
		}
		select {
		case <-c.Closed():
			s.c = nil
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue // retry once on a fresh connection
		default:
			return err
		}
	}
	return errors.New("bağlantı koptu")
}

// doList runs f on the list session, or on the prefetch session when list
// is already busy, so two header pages can overlap on the server.
func (b *IMAP) doList(ctx context.Context, f func(s *session, c *imapclient.Client) error) error {
	if b.pref != nil && !b.list.mu.TryLock() {
		return b.pref.do(ctx, func(c *imapclient.Client) error { return f(b.pref, c) })
	}
	if b.pref != nil {
		// TryLock succeeded: run under that lock (do() would deadlock).
		defer b.list.mu.Unlock()
		return b.list.doLocked(ctx, func(c *imapclient.Client) error { return f(b.list, c) })
	}
	return b.list.do(ctx, func(c *imapclient.Client) error { return f(b.list, c) })
}

func (s *session) selectLocked(c *imapclient.Client, folder string) (*imap.SelectData, error) {
	var opts *imap.SelectOptions
	if hasCondstore(c) {
		opts = &imap.SelectOptions{CondStore: true}
	}
	d, err := c.Select(folder, opts).Wait()
	if err != nil {
		s.selected = ""
		return nil, err
	}
	s.selected = folder
	s.b.rememberSync(folder, d)
	return d, nil
}

func hasCondstore(c *imapclient.Client) bool {
	return c != nil && (c.Caps().Has(imap.CapCondStore) || c.Caps().Has(imap.CapQResync))
}

func (b *IMAP) rememberSync(folder string, d *imap.SelectData) {
	if b == nil || d == nil {
		return
	}
	b.syncMu.Lock()
	if b.folderSync == nil {
		b.folderSync = map[string]FolderSync{}
	}
	b.folderSync[folder] = FolderSync{Folder: folder, UIDValidity: d.UIDValidity, UIDNext: uint32(d.UIDNext), HighestModSeq: d.HighestModSeq}
	b.syncMu.Unlock()
}

// LastFolderSync implements IncrementalSyncer.
func (b *IMAP) LastFolderSync(folder string) FolderSync {
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	return b.folderSync[folder]
}

func (s *session) ensureSelected(c *imapclient.Client, folder string) error {
	if s.selected == folder {
		return nil
	}
	_, err := s.selectLocked(c, folder)
	return err
}

// Folders lists mailboxes with unread counts.
func (b *IMAP) Folders(ctx context.Context) ([]Folder, error) {
	var out []Folder
	err := b.main.do(ctx, func(c *imapclient.Client) error {
		list, err := c.List("", "*", nil).Collect()
		if err != nil {
			return err
		}
		out = out[:0]
		for _, l := range list {
			f := Folder{Name: l.Mailbox, Delim: l.Delim}
			skip := false
			for _, a := range l.Attrs {
				switch a {
				case imap.MailboxAttrNoSelect, imap.MailboxAttrNonExistent:
					skip = true
				case imap.MailboxAttrSent:
					f.Role = RoleSent
				case imap.MailboxAttrDrafts:
					f.Role = RoleDrafts
				case imap.MailboxAttrTrash:
					f.Role = RoleTrash
				case imap.MailboxAttrJunk:
					f.Role = RoleJunk
				case imap.MailboxAttrArchive:
					f.Role = RoleArchive
				case imap.MailboxAttrFlagged:
					f.Role = RoleFlagged
				case imap.MailboxAttrAll:
					f.Role = RoleAll
				case imap.MailboxAttrImportant:
					skip = true // Gmail "Önemli": not useful in a flat client
				}
			}
			if skip {
				continue
			}
			if strings.EqualFold(l.Mailbox, "INBOX") {
				f.Role = RoleInbox
			}
			if f.Role == RoleNone {
				f.Role = RoleByName(l.Mailbox)
			}
			out = append(out, f)
		}
		dedupeRoles(out)
		for i := range out {
			if i >= 60 {
				break
			}
			st, err := c.Status(out[i].Name, &imap.StatusOptions{NumMessages: true, NumUnseen: true}).Wait()
			if err != nil {
				continue
			}
			if st.NumUnseen != nil {
				out[i].Unread = int(*st.NumUnseen)
			}
			if st.NumMessages != nil {
				out[i].Total = int(*st.NumMessages)
			}
		}
		return nil
	})
	return out, err
}

// dedupeRoles keeps only the first folder of each role (others become custom).
func dedupeRoles(fs []Folder) {
	seen := map[Role]bool{}
	for i := range fs {
		if fs[i].Role == RoleNone {
			continue
		}
		if seen[fs[i].Role] {
			fs[i].Role = RoleNone
			continue
		}
		seen[fs[i].Role] = true
	}
}

var listFetch = &imap.FetchOptions{Envelope: true, Flags: true, UID: true, InternalDate: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}

func toEnvelope(m *imapclient.FetchMessageBuffer) Envelope {
	e := Envelope{UID: uint32(m.UID), Date: m.InternalDate}
	if m.Envelope != nil {
		e.Subject = m.Envelope.Subject
		if !m.Envelope.Date.IsZero() {
			e.Date = m.Envelope.Date
		}
		e.MessageID = m.Envelope.MessageID
		if len(m.Envelope.From) > 0 {
			a := m.Envelope.From[0]
			e.FromAddr = a.Addr()
			e.From = a.Name
			if e.From == "" {
				e.From = e.FromAddr
			}
		}
		if len(m.Envelope.To) > 0 {
			a := m.Envelope.To[0]
			e.ToAddr = a.Addr()
			e.To = a.Name
			if e.To == "" {
				e.To = e.ToAddr
			}
		}
		e.ToPeople = addressHay(m.Envelope.To) + " " + addressHay(m.Envelope.Cc)
	}
	for _, f := range m.Flags {
		switch f {
		case imap.FlagSeen:
			e.Seen = true
		case imap.FlagFlagged:
			e.Flagged = true
		}
	}
	e.Attach = hasAttachment(m.BodyStructure)
	return e
}

func hasAttachment(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	found := false
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		if sp, ok := part.(*imap.BodyStructureSinglePart); ok {
			if d := sp.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
				found = true
			} else if sp.Filename() != "" && !strings.HasPrefix(sp.MediaType(), "text/") {
				found = true
			}
		}
		return !found
	})
	return found
}

// List returns the newest `limit` messages of a folder.
func (b *IMAP) List(ctx context.Context, folder string, limit int) ([]Envelope, error) {
	var out []Envelope
	err := b.list.do(ctx, func(c *imapclient.Client) error {
		d, err := b.list.selectLocked(c, folder) // re-select to refresh counts
		if err != nil {
			return err
		}
		out = nil
		if d.NumMessages == 0 {
			return nil
		}
		start := uint32(1)
		if limit > 0 && d.NumMessages > uint32(limit) {
			start = d.NumMessages - uint32(limit) + 1
		}
		var set imap.SeqSet
		set.AddRange(start, d.NumMessages)
		msgs, err := c.Fetch(set, listFetch).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			out = append(out, toEnvelope(m))
		}
		return nil
	})
	sortNewest(out)
	return out, err
}

// ListPage returns a newest-first (by date) window of a folder and the mailbox size.
func (b *IMAP) ListPage(ctx context.Context, folder string, offset, limit int) ([]Envelope, int, error) {
	return b.listWindow(ctx, folder, 0, offset, limit)
}

// ListFlagPage returns a newest-first (by date) window of unseen or seen messages.
func (b *IMAP) ListFlagPage(ctx context.Context, folder string, unseen bool, offset, limit int) ([]Envelope, int, error) {
	mode := 1
	if !unseen {
		mode = 2
	}
	return b.listWindow(ctx, folder, mode, offset, limit)
}

func sortNewest(l []Envelope) {
	sort.SliceStable(l, func(i, j int) bool {
		if l[i].Date.Equal(l[j].Date) {
			return l[i].UID > l[j].UID
		}
		return l[i].Date.After(l[j].Date)
	})
}

// FetchRaw downloads the full message without marking it read.
func (b *IMAP) FetchRaw(ctx context.Context, folder string, uid uint32) ([]byte, error) {
	var raw []byte
	err := b.body.do(ctx, func(c *imapclient.Client) error {
		if err := b.body.ensureSelected(c, folder); err != nil {
			return err
		}
		sec := &imap.FetchItemBodySection{Peek: true}
		msgs, err := c.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return errors.New("ileti bulunamadı")
		}
		raw = msgs[0].FindBodySection(sec)
		return nil
	})
	return raw, err
}

// SetFlag adds or removes a flag.
func (b *IMAP) SetFlag(ctx context.Context, folder string, uid uint32, f Flag, on bool) error {
	return b.ops.do(ctx, func(c *imapclient.Client) error {
		if err := b.ops.ensureSelected(c, folder); err != nil {
			return err
		}
		op := imap.StoreFlagsAdd
		if !on {
			op = imap.StoreFlagsDel
		}
		msgs, err := c.Store(imap.UIDSetNum(imap.UID(uid)), &imap.StoreFlags{Op: op, Flags: []imap.Flag{imap.Flag(f)}}, nil).Collect()
		if err != nil {
			return err
		}
		return verifyFlags(c, []uint32{uid}, msgs, f, on)
	})
}

// Move moves a message, returning its new UID when the server reports it.
func (b *IMAP) Move(ctx context.Context, folder string, uid uint32, dest, messageID string) (uint32, error) {
	var newUID uint32
	err := b.ops.do(ctx, func(c *imapclient.Client) error {
		if err := b.ops.ensureSelected(c, folder); err != nil {
			return err
		}
		md, err := c.Move(imap.UIDSetNum(imap.UID(uid)), dest).Wait()
		if err != nil {
			return err
		}
		if md != nil && md.DestUIDs != nil {
			if us, ok := md.DestUIDs.(imap.UIDSet); ok {
				if nums, ok := us.Nums(); ok && len(nums) > 0 {
					newUID = uint32(nums[0])
				}
			}
		}
		if newUID == 0 && messageID != "" {
			// Fallback: find it by Message-ID in the destination.
			if _, err := b.ops.selectLocked(c, dest); err == nil {
				sd, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: messageID}}}, nil).Wait()
				if err == nil {
					if u := sd.AllUIDs(); len(u) > 0 {
						newUID = uint32(u[len(u)-1])
					}
				}
			}
		}
		return nil
	})
	return newUID, err
}

// Delete permanently removes a message.
func (b *IMAP) Delete(ctx context.Context, folder string, uid uint32) error {
	return b.ops.do(ctx, func(c *imapclient.Client) error {
		if err := b.ops.ensureSelected(c, folder); err != nil {
			return err
		}
		set := imap.UIDSetNum(imap.UID(uid))
		if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
			return err
		}
		if c.Caps().Has(imap.CapUIDPlus) || c.Caps().Has(imap.CapIMAP4rev2) {
			return c.UIDExpunge(set).Close()
		}
		return c.Expunge().Close()
	})
}

// MoveMany moves several messages of one folder with one UID MOVE (the
// library falls back to UID COPY + STORE \Deleted + (UID) EXPUNGE when the
// server lacks MOVE). It returns old UID -> new UID where reported.
func (b *IMAP) MoveMany(ctx context.Context, folder string, uids []uint32, dest string) (map[uint32]uint32, error) {
	out := map[uint32]uint32{}
	err := b.ops.do(ctx, func(c *imapclient.Client) error {
		if err := b.ops.ensureSelected(c, folder); err != nil {
			return err
		}
		md, err := c.Move(uidSet(uids), dest).Wait()
		if err != nil {
			return err
		}
		if md != nil && md.SourceUIDs != nil && md.DestUIDs != nil {
			src, ok1 := md.SourceUIDs.(imap.UIDSet)
			dst, ok2 := md.DestUIDs.(imap.UIDSet)
			if ok1 && ok2 {
				a, okA := src.Nums()
				d, okD := dst.Nums()
				if okA && okD && len(a) == len(d) {
					for i := range a {
						out[uint32(a[i])] = uint32(d[i])
					}
				}
			}
		}
		return nil
	})
	return out, err
}

// SetFlagMany adds or removes a flag on several messages at once.
func (b *IMAP) SetFlagMany(ctx context.Context, folder string, uids []uint32, f Flag, on bool) error {
	return b.ops.do(ctx, func(c *imapclient.Client) error {
		if err := b.ops.ensureSelected(c, folder); err != nil {
			return err
		}
		op := imap.StoreFlagsAdd
		if !on {
			op = imap.StoreFlagsDel
		}
		// Not .SILENT: the server answers with each mail's new FLAGS, so a
		// STORE that "succeeds" without touching anything (a UID that is no
		// longer there) is caught instead of being undone by the next sync.
		msgs, err := c.Store(uidSet(uids), &imap.StoreFlags{Op: op, Flags: []imap.Flag{imap.Flag(f)}}, nil).Collect()
		if err != nil {
			return err
		}
		return verifyFlags(c, uids, msgs, f, on)
	})
}

// ErrFlagNotApplied: the server said OK but the mail's flags did not change.
var ErrFlagNotApplied = errors.New("sunucu işareti uygulamadı")

// ErrMsgGone: the mail is no longer in the folder on the server.
var ErrMsgGone = errors.New("ileti sunucuda bulunamadı")

// verifyFlags checks the FETCH FLAGS answers of a STORE; mails the server
// stayed quiet about are asked with UID FETCH (FLAGS).
func verifyFlags(c *imapclient.Client, uids []uint32, msgs []*imapclient.FetchMessageBuffer, f Flag, on bool) error {
	state := map[uint32]bool{}
	note := func(ms []*imapclient.FetchMessageBuffer) {
		for _, m := range ms {
			if m.UID == 0 {
				continue
			}
			has := false
			for _, x := range m.Flags {
				if strings.EqualFold(string(x), string(f)) {
					has = true
				}
			}
			state[uint32(m.UID)] = has
		}
	}
	note(msgs)
	var quiet []uint32
	for _, u := range uids {
		if _, ok := state[u]; !ok {
			quiet = append(quiet, u)
		}
	}
	if len(quiet) > 0 {
		ms, err := c.Fetch(uidSet(quiet), &imap.FetchOptions{UID: true, Flags: true}).Collect()
		if err != nil {
			return err
		}
		note(ms)
	}
	for _, u := range uids {
		has, ok := state[u]
		if !ok {
			return ErrMsgGone
		}
		if has != on {
			return ErrFlagNotApplied
		}
	}
	return nil
}

func uidSet(uids []uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, u := range uids {
		set.AddNum(imap.UID(u))
	}
	return set
}

// Count returns the folder's current number of messages.
func (b *IMAP) Count(ctx context.Context, folder string) (int, error) {
	n := 0
	err := b.ops.do(ctx, func(c *imapclient.Client) error {
		if err := b.ops.ensureSelected(c, folder); err != nil {
			return err
		}
		sd, err := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
		if err != nil {
			return err
		}
		n = len(sd.AllUIDs())
		return nil
	})
	return n, err
}

// FolderStatus reports one mailbox's message and unseen counts (STATUS).
// It uses the ops connection so a listing on main is not blocked.
func (b *IMAP) FolderStatus(ctx context.Context, folder string) (total, unread int, err error) {
	err = b.ops.do(ctx, func(c *imapclient.Client) error {
		st, err := c.Status(folder, &imap.StatusOptions{NumMessages: true, NumUnseen: true}).Wait()
		if err != nil {
			return err
		}
		if st != nil {
			if st.NumMessages != nil {
				total = int(*st.NumMessages)
			}
			if st.NumUnseen != nil {
				unread = int(*st.NumUnseen)
			}
		}
		return nil
	})
	return total, unread, err
}

// Empty permanently removes every message in folder: UID STORE +FLAGS.SILENT
// (\Deleted) and UID EXPUNGE (plain EXPUNGE without UIDPLUS), in batches so
// big folders show progress and never send one huge command.
func (b *IMAP) Empty(ctx context.Context, folder string, progress func(done, total int)) error {
	return b.ops.do(ctx, func(c *imapclient.Client) error {
		if err := b.ops.ensureSelected(c, folder); err != nil {
			return err
		}
		sd, err := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
		if err != nil {
			return err
		}
		var uids []uint32
		for _, u := range sd.AllUIDs() {
			uids = append(uids, uint32(u))
		}
		uidPlus := c.Caps().Has(imap.CapUIDPlus) || c.Caps().Has(imap.CapIMAP4rev2)
		done := 0
		for _, batch := range Batches(uids, EmptyBatch) {
			if err := ctx.Err(); err != nil {
				return err
			}
			var set imap.UIDSet
			for _, u := range batch {
				set.AddNum(imap.UID(u))
			}
			if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
				return err
			}
			if uidPlus {
				err = c.UIDExpunge(set).Close()
			} else {
				err = c.Expunge().Close()
			}
			if err != nil {
				return err
			}
			done += len(batch)
			if progress != nil {
				progress(done, len(uids))
			}
		}
		return nil
	})
}

// Append stores a message in a folder.
func (b *IMAP) Append(ctx context.Context, folder string, raw []byte, flags []Flag) error {
	return b.main.do(ctx, func(c *imapclient.Client) error {
		var fl []imap.Flag
		for _, f := range flags {
			fl = append(fl, imap.Flag(f))
		}
		cmd := c.Append(folder, int64(len(raw)), &imap.AppendOptions{Flags: fl, Time: time.Now()})
		if _, err := cmd.Write(raw); err != nil {
			cmd.Close()
			return err
		}
		if err := cmd.Close(); err != nil {
			return err
		}
		_, err := cmd.Wait()
		return err
	})
}

// CreateFolder creates a mailbox (e.g. "Spam" when the account has none).
func (b *IMAP) CreateFolder(ctx context.Context, name string) error {
	return b.main.do(ctx, func(c *imapclient.Client) error {
		return c.Create(name, nil).Wait()
	})
}

// Copy copies one message to dest (UID COPY) and returns its new UID when known.
func (b *IMAP) Copy(ctx context.Context, folder string, uid uint32, dest, messageID string) (uint32, error) {
	var newUID uint32
	err := b.ops.do(ctx, func(c *imapclient.Client) error {
		if err := b.ops.ensureSelected(c, folder); err != nil {
			return err
		}
		cd, err := c.Copy(imap.UIDSetNum(imap.UID(uid)), dest).Wait()
		if err != nil {
			return err
		}
		if cd != nil {
			if nums, ok := cd.DestUIDs.Nums(); ok && len(nums) > 0 {
				newUID = uint32(nums[0])
			}
		}
		if newUID == 0 && messageID != "" {
			if _, err := b.ops.selectLocked(c, dest); err == nil {
				sd, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: messageID}}}, nil).Wait()
				if err == nil {
					if u := sd.AllUIDs(); len(u) > 0 {
						newUID = uint32(u[len(u)-1])
					}
				}
			}
		}
		return nil
	})
	return newUID, err
}

func (s *session) dropSelect(c *imapclient.Client, name string) {
	if s.selected != name {
		return
	}
	_ = c.Unselect().Wait()
	s.selected = ""
}

// DeleteFolder empties a mailbox then issues IMAP DELETE.
func (b *IMAP) DeleteFolder(ctx context.Context, name string) error {
	if err := b.Empty(ctx, name, nil); err != nil {
		// a missing or already-empty mailbox is still deleted below
		if !strings.Contains(strings.ToLower(err.Error()), "no such") && !strings.Contains(strings.ToLower(err.Error()), "does not exist") {
			// keep going: some servers reject SELECT of an empty leftover
		}
	}
	return b.ops.do(ctx, func(c *imapclient.Client) error {
		b.ops.dropSelect(c, name)
		return c.Delete(name).Wait()
	})
}

// RenameFolder issues IMAP RENAME.
func (b *IMAP) RenameFolder(ctx context.Context, oldName, newName string) error {
	return b.ops.do(ctx, func(c *imapclient.Client) error {
		b.ops.dropSelect(c, oldName)
		return c.Rename(oldName, newName, nil).Wait()
	})
}

// Search runs a server-side text search in a folder.
func (b *IMAP) Search(ctx context.Context, folder, query string, limit int) ([]Envelope, error) {
	var out []Envelope
	err := b.main.do(ctx, func(c *imapclient.Client) error {
		if err := b.main.ensureSelected(c, folder); err != nil {
			return err
		}
		sd, err := c.UIDSearch(&imap.SearchCriteria{Text: []string{query}}, &imap.SearchOptions{}).Wait()
		if err != nil {
			return err
		}
		uids := sd.AllUIDs()
		if len(uids) == 0 {
			return nil
		}
		if limit > 0 && len(uids) > limit {
			uids = uids[len(uids)-limit:]
		}
		msgs, err := c.Fetch(imap.UIDSetNum(uids...), listFetch).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			out = append(out, toEnvelope(m))
		}
		return nil
	})
	sortNewest(out)
	return out, err
}

// Flagged returns the flagged messages of a folder (UID SEARCH FLAGGED).
func (b *IMAP) Flagged(ctx context.Context, folder string, limit int) ([]Envelope, error) {
	var out []Envelope
	err := b.main.do(ctx, func(c *imapclient.Client) error {
		if err := b.main.ensureSelected(c, folder); err != nil {
			return err
		}
		sd, err := c.UIDSearch(&imap.SearchCriteria{Flag: []imap.Flag{imap.FlagFlagged}}, &imap.SearchOptions{}).Wait()
		if err != nil {
			return err
		}
		uids := sd.AllUIDs()
		if len(uids) == 0 {
			return nil
		}
		if limit > 0 && len(uids) > limit {
			uids = uids[len(uids)-limit:]
		}
		msgs, err := c.Fetch(imap.UIDSetNum(uids...), listFetch).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			out = append(out, toEnvelope(m))
		}
		return nil
	})
	sortNewest(out)
	return out, err
}

// Send delivers a message and saves a copy to sentFolder.
// smtpFresh is how long an idle outgoing connection is reused.
const smtpFresh = 2 * time.Minute

// Prewarm opens and signs in the outgoing connection ahead of a send.
func (b *IMAP) Prewarm(ctx context.Context) error {
	b.smtpMu.Lock()
	defer b.smtpMu.Unlock()
	_, err := b.smtpClientLocked(ctx)
	return err
}

func (b *IMAP) smtpClientLocked(ctx context.Context) (*smtp.Client, error) {
	if b.smtpC != nil {
		if time.Since(b.smtpAt) < smtpFresh && b.smtpC.Noop() == nil {
			return b.smtpC, nil
		}
		b.smtpC.Close()
		b.smtpC = nil
	}
	c, err := dialSMTP(ctx, b.cfg)
	if err != nil {
		return nil, err
	}
	b.smtpC, b.smtpAt = c, time.Now()
	return c, nil
}

func (b *IMAP) dropSMTPLocked() {
	if b.smtpC != nil {
		b.smtpC.Close()
		b.smtpC = nil
	}
}

// ServerSavesSent reports servers that store a copy of every sent mail by
// themselves (Gmail, Outlook / Microsoft 365): appending our own copy
// would create a duplicate in Sent.
func (b *IMAP) ServerSavesSent() bool {
	h := strings.ToLower(b.cfg.Incoming.Host + " " + b.cfg.Outgoing.Host)
	for _, s := range []string{"gmail.com", "googlemail.com", "office365.com", "outlook.com"} {
		if strings.Contains(h, s) {
			return true
		}
	}
	b.main.mu.Lock()
	defer b.main.mu.Unlock()
	return b.gmail || (b.main.c != nil && b.main.c.Caps().Has("X-GM-EXT-1"))
}

func (b *IMAP) Send(ctx context.Context, from string, rcpts []string, raw []byte, sentFolder string) error {
	b.smtpMu.Lock()
	err := b.sendLocked(ctx, from, rcpts, raw, true)
	b.smtpMu.Unlock()
	if err != nil {
		return err
	}
	if sentFolder != "" && !b.ServerSavesSent() {
		if err := b.Append(ctx, sentFolder, raw, []Flag{FlagSeen}); err != nil {
			return fmt.Errorf("%w: %v", ErrSentCopy, err)
		}
	}
	return nil
}

func (b *IMAP) sendLocked(ctx context.Context, from string, rcpts []string, raw []byte, retry bool) error {
	reused := b.smtpC != nil
	c, err := b.smtpClientLocked(ctx)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	err = c.SendMail(from, rcpts, bytes.NewReader(raw))
	stop()
	if err != nil {
		var se *smtp.SMTPError
		b.dropSMTPLocked()
		if reused && retry && !errors.As(err, &se) {
			return b.sendLocked(ctx, from, rcpts, raw, false) // stale connection: once more, fresh
		}
		return err
	}
	b.smtpAt = time.Now()
	return nil
}

// ErrSentCopy: the mail went out, only the copy in Sent failed.
var ErrSentCopy = errors.New("gönderildi ama kopyası kaydedilemedi")

func dialSMTP(ctx context.Context, cfg Config) (*smtp.Client, error) {
	s := cfg.Outgoing
	if s.Host == "" {
		return nil, errors.New("giden sunucu ayarlı değil")
	}
	tc := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	var (
		c   *smtp.Client
		err error
	)
	switch s.Security {
	case discover.TLS:
		c, err = smtp.DialTLS(s.Addr(), tc)
	case discover.StartTLS:
		c, err = smtp.DialStartTLS(s.Addr(), tc)
	case discover.None:
		if !isLoopback(s.Host) {
			return nil, errors.New("güvensiz bağlantı reddedildi")
		}
		c, err = smtp.Dial(s.Addr())
	default:
		return nil, fmt.Errorf("bilinmeyen bağlantı türü %q", s.Security)
	}
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	var sc sasl.Client
	if cfg.Token != nil {
		tok, err := cfg.Token(ctx)
		if err != nil {
			c.Close()
			return nil, err
		}
		sc = auth.NewXOAUTH2(cfg.Username, tok)
	} else {
		sc = sasl.NewPlainClient("", cfg.Username, cfg.Password)
	}
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(sc); err != nil {
			c.Close()
			return nil, fmt.Errorf("%w: %v", ErrAuth, err)
		}
	}
	return c, nil
}

// Watch keeps a second connection idling on the inbox and reports new mail.
// Servers without push support are checked every minute.
func (b *IMAP) Watch(ctx context.Context, inbox string, notify func(Event)) {
	backoff := 2 * time.Second
	online := false
	for ctx.Err() == nil {
		err := b.watchOnce(ctx, inbox, func(e Event) {
			if !online && e.Kind == EvOnline {
				online = true
				backoff = 2 * time.Second
			}
			notify(e)
		})
		if ctx.Err() != nil {
			return
		}
		if online || err != nil {
			online = false
			notify(Event{Kind: EvOffline, Err: err})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 2*time.Minute)
	}
}

func (b *IMAP) watchOnce(ctx context.Context, inbox string, notify func(Event)) error {
	var mu sync.Mutex
	var last uint32
	var ready bool
	h := &imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages == nil {
				return
			}
			mu.Lock()
			n := *d.NumMessages
			grew := ready && n > last
			last = n
			mu.Unlock()
			if grew {
				notify(Event{Kind: EvNewMail, Folder: inbox})
			}
		},
		Expunge: func(uint32) {
			mu.Lock()
			if last > 0 {
				last--
			}
			mu.Unlock()
			notify(Event{Kind: EvChanged, Folder: inbox})
		},
	}
	c, err := b.dial(h)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err := b.login(ctx, c); err != nil {
		return err
	}
	d, err := c.Select(inbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return err
	}
	mu.Lock()
	last, ready = d.NumMessages, true
	mu.Unlock()

	if c.Caps().Has(imap.CapIdle) || c.Caps().Has(imap.CapIMAP4rev2) {
		idle, err := c.Idle()
		if err != nil {
			return err
		}
		// Reported only once push is active, so the UI refresh that follows
		// cannot miss a message delivered in between.
		notify(Event{Kind: EvOnline, Folder: inbox})
		select {
		case <-ctx.Done():
			idle.Close()
			return nil
		case <-c.Closed():
			return errors.New("bağlantı koptu")
		}
	}
	notify(Event{Kind: EvOnline, Folder: inbox})
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.Closed():
			return errors.New("bağlantı koptu")
		case <-t.C:
			if err := c.Noop().Wait(); err != nil {
				return err
			}
		}
	}
}

// Close drops every connection at once: no LOGOUT, no IDLE DONE, no SMTP
// QUIT and no waiting for a busy session (the program is exiting; the
// server cleans up by itself). It returns within closeWait even when a
// server has stalled.
func (b *IMAP) Close() error {
	b.closed.Store(true)
	var wg sync.WaitGroup
	if b.smtpMu.TryLock() { // (a send in progress keeps its connection; the process ends anyway)
		if c := b.smtpC; c != nil {
			b.smtpC = nil
			wg.Add(1)
			go func() { defer wg.Done(); c.Close() }()
		}
		b.smtpMu.Unlock()
	}
	b.liveMu.Lock()
	for c := range b.live {
		wg.Add(1)
		go func(c *imapclient.Client) { defer wg.Done(); c.Close() }(c)
	}
	b.liveMu.Unlock()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(closeWait):
	}
	return nil
}

// closeWait bounds Close (closing a TLS connection may try to send a
// goodbye to a server that no longer reads).
const closeWait = 150 * time.Millisecond

// DebugOut receives the raw protocol trace when TERMAIL_DEBUG_IDLE is set.
// It must never be the terminal: stray output scrolls the screen. main
// points it at the log file.
var DebugOut io.Writer

func debugW(on bool) io.Writer {
	if on && DebugOut != nil && os.Getenv("TERMAIL_DEBUG_IDLE") != "" {
		return DebugOut
	}
	return nil
}

func addressHay(as []imap.Address) string {
	var b strings.Builder
	for _, a := range as {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		if a.Name != "" {
			b.WriteString(a.Name)
			b.WriteByte(' ')
		}
		b.WriteString(a.Addr())
	}
	return strings.TrimSpace(b.String())
}
