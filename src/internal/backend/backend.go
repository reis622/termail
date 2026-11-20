// Package backend talks to mail servers. The UI only sees the Backend
// interface; a real (IMAP + SMTP) and an in-memory demo implementation exist.
package backend

import (
	"context"
	"strings"

	"termail/internal/i18n"
	"time"
)

// Role of a special folder.
type Role string

const (
	RoleNone    Role = ""
	RoleInbox   Role = "inbox"
	RoleFlagged Role = "flagged"
	RoleSent    Role = "sent"
	RoleDrafts  Role = "drafts"
	RoleArchive Role = "archive"
	RoleJunk    Role = "junk"
	RoleTrash   Role = "trash"
	RoleAll     Role = "all"
)

// RoleOrder is the display order of standard folders.
var RoleOrder = []Role{RoleInbox, RoleFlagged, RoleSent, RoleDrafts, RoleArchive, RoleJunk, RoleTrash}

// DisplayName returns the Turkish name for a role.
func (r Role) DisplayName() string {
	switch r {
	case RoleInbox:
		return i18n.T("Gelen Kutusu")
	case RoleFlagged:
		return i18n.T("Yıldızlı")
	case RoleSent:
		return i18n.T("Gönderilenler")
	case RoleDrafts:
		return i18n.T("Taslaklar")
	case RoleArchive:
		return i18n.T("Arşiv")
	case RoleJunk:
		return i18n.T("Spam")
	case RoleTrash:
		return i18n.T("Çöp Kutusu")
	case RoleAll:
		return i18n.T("Tüm postalar")
	}
	return ""
}

// Folder is a mailbox.
type Folder struct {
	Name   string // server name (used for commands)
	Role   Role
	Unread int
	Total  int
	Delim  rune
}

// Label is the user-facing name.
func (f Folder) Label() string {
	if n := f.Role.DisplayName(); n != "" {
		return n
	}
	name := f.Name
	if f.Delim != 0 {
		if i := strings.LastIndexByte(name, byte(f.Delim)); i >= 0 && f.Delim < 128 {
			name = name[i+1:]
		}
	}
	return name
}

// Envelope is a message summary for the list.
type Envelope struct {
	UID       uint32
	From      string // display name
	FromAddr  string
	To        string // first To display name
	ToAddr    string
	ToPeople  string // names and addresses of To/Cc, for kime:/to: search
	Subject   string
	Date      time.Time
	Seen      bool
	Flagged   bool
	Attach    bool
	MessageID string
	// Acct and Folder are filled in by the UI for merged lists (all
	// inboxes, saved mail). A backend sets Folder only when a result really
	// lives in another folder (e.g. a virtual "flagged" folder).
	Acct   int
	Folder string
	// ReadAt is when the user last opened this mail (UI-only, not IMAP).
	// Okunanlar is sorted by it, newest first.
	ReadAt time.Time
}

// Flag names used by SetFlag.
type Flag string

const (
	FlagSeen    Flag = `\Seen`
	FlagFlagged Flag = `\Flagged`
	FlagDraft   Flag = `\Draft`
)

// EventKind of a watcher event.
type EventKind int

const (
	EvNewMail EventKind = iota // folder may have new messages
	EvChanged                  // something else changed (expunge, flags)
	EvOnline
	EvOffline
)

// Event comes from Watch.
type Event struct {
	Kind     EventKind
	Folder   string
	Err      error
	NewUIDs  []uint32   // EXISTS: new messages (when known)
	Vanished []uint32   // expunged UIDs (when known)
	Flags    []Envelope // IDLE FETCH flag updates (UID + Seen/Flagged)
}

// Backend is one connected account.
type Backend interface {
	Connect(ctx context.Context) error
	Folders(ctx context.Context) ([]Folder, error)
	List(ctx context.Context, folder string, limit int) ([]Envelope, error) // newest first
	FetchRaw(ctx context.Context, folder string, uid uint32) ([]byte, error)
	SetFlag(ctx context.Context, folder string, uid uint32, f Flag, on bool) error
	// Move moves a message and returns its UID in dest (0 if unknown).
	Move(ctx context.Context, folder string, uid uint32, dest, messageID string) (uint32, error)
	// MoveMany moves several messages of one folder in one step and returns
	// old UID -> new UID where the server reports it.
	MoveMany(ctx context.Context, folder string, uids []uint32, dest string) (map[uint32]uint32, error)
	// SetFlagMany changes a flag on several messages of one folder.
	SetFlagMany(ctx context.Context, folder string, uids []uint32, f Flag, on bool) error
	// Delete removes a message permanently.
	Delete(ctx context.Context, folder string, uid uint32) error
	// Count returns how many messages a folder holds right now (server truth).
	Count(ctx context.Context, folder string) (int, error)
	// Empty permanently removes every message of a folder, in batches;
	// progress reports done/total after each batch.
	Empty(ctx context.Context, folder string, progress func(done, total int)) error
	Append(ctx context.Context, folder string, raw []byte, flags []Flag) error
	CreateFolder(ctx context.Context, name string) error
	// Copy copies a message to dest and returns its UID there (0 if unknown).
	Copy(ctx context.Context, folder string, uid uint32, dest, messageID string) (uint32, error)
	// DeleteFolder removes a mailbox and its messages (IMAP DELETE).
	DeleteFolder(ctx context.Context, name string) error
	// RenameFolder renames a mailbox (IMAP RENAME).
	RenameFolder(ctx context.Context, oldName, newName string) error
	Search(ctx context.Context, folder, query string, limit int) ([]Envelope, error)
	// Flagged lists the flagged ("saved") messages of a folder, newest first.
	Flagged(ctx context.Context, folder string, limit int) ([]Envelope, error)
	// Send delivers raw to rcpts and stores a copy in sentFolder when non-empty.
	Send(ctx context.Context, from string, rcpts []string, raw []byte, sentFolder string) error
	// Prewarm opens and signs in the outgoing connection ahead of a send.
	Prewarm(ctx context.Context) error
	// Watch blocks, reporting new mail in the inbox until ctx is cancelled.
	Watch(ctx context.Context, inbox string, notify func(Event))
	Close() error
}

// FolderStatuser is an optional backend that can STATUS one mailbox
// (total and unseen) without listing every folder.
type FolderStatuser interface {
	FolderStatus(ctx context.Context, folder string) (total, unread int, err error)
}

// PagedLister fetches a folder in windows, newest first, so the UI can
// show what it already has and load the rest in the background.
type PagedLister interface {
	// ListPage returns up to limit messages starting offset from the newest
	// (offset 0 = newest) and the mailbox size.
	ListPage(ctx context.Context, folder string, offset, limit int) (envs []Envelope, total int, err error)
	// ListFlagPage is ListPage for unseen (unseen=true) or seen messages.
	// total is the number of matching messages.
	ListFlagPage(ctx context.Context, folder string, unseen bool, offset, limit int) (envs []Envelope, total int, err error)
}

// FolderSync is the persisted CONDSTORE/QRESYNC bookmark of one folder.
type FolderSync struct {
	Folder        string
	UIDValidity   uint32
	UIDNext       uint32
	HighestModSeq uint64
}

// SyncDelta is the incremental change of a folder since FolderSync.
type SyncDelta struct {
	New       []Envelope // messages not in the local list
	Flags     []Envelope // UID + updated flags
	Vanished  []uint32
	FullReset bool // UIDVALIDITY changed (or the mailbox was rebuilt)
	Sync      FolderSync
}

// IncrementalSyncer is an optional backend that can update a cached folder
// without listing it from scratch.
type IncrementalSyncer interface {
	SyncFolder(ctx context.Context, state FolderSync, known []uint32) (SyncDelta, error)
	LastFolderSync(folder string) FolderSync
}

// RoleByName guesses a role from common folder names (when the server does
// not mark special folders).
func RoleByName(name string) Role {
	n := strings.ToLower(name)
	if i := strings.LastIndexAny(n, "/."); i >= 0 && !strings.EqualFold(name, "INBOX") {
		n = n[i+1:]
	}
	switch n {
	case "inbox":
		return RoleInbox
	case "sent", "sent items", "sent messages", "sent mail", "gönderilenler", "gönderilmiş öğeler", "gönderilmiş", "giden":
		return RoleSent
	case "drafts", "draft", "taslaklar", "taslak":
		return RoleDrafts
	case "trash", "deleted", "deleted items", "deleted messages", "bin", "çöp", "çöp kutusu", "silinmiş öğeler", "silinenler":
		return RoleTrash
	case "junk", "spam", "junk e-mail", "junk email", "gereksiz", "gereksiz e-posta", "istenmeyen":
		return RoleJunk
	case "archive", "archives", "arşiv":
		return RoleArchive
	case "starred", "flagged", "yıldızlı":
		return RoleFlagged
	case "all mail", "tüm postalar", "tüm e-postalar":
		return RoleAll
	}
	return RoleNone
}

// NonMailFolder reports Outlook / Exchange folders that are not mailboxes
// (Outbox, Notes, Calendar, Contacts, …) and should stay off the folder list.
func NonMailFolder(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if i := strings.LastIndexAny(n, "/."); i >= 0 {
		n = n[i+1:]
	}
	switch n {
	case "outbox", "giden kutusu", "out-box",
		"notes", "notlar", "notlarım", "sticky notes",
		"journal", i18n.T("günlük"), "diary",
		"contacts", i18n.T("kişiler"), "people", "rehber", "suggested contacts",
		"calendar", "takvim", "calendars",
		"tasks", "görevler", "to-do", "todo", "to do",
		"sync issues", "eşitleme sorunları", "conflicts", "çatışmalar",
		"conversation history", "konuşma geçmişi",
		"rss feeds", "rss", "rss subscriptions",
		"clutter", "focused", "other":
		return true
	}
	return false
}

// EmptyBatch is how many messages one "empty folder" step removes.
const EmptyBatch = 250

// Batches splits uids into chunks of at most n (n <= 0 means one chunk).
func Batches(uids []uint32, n int) [][]uint32 {
	if len(uids) == 0 {
		return nil
	}
	if n <= 0 {
		n = len(uids)
	}
	var out [][]uint32
	for len(uids) > 0 {
		k := min(n, len(uids))
		out = append(out, uids[:k:k])
		uids = uids[k:]
	}
	return out
}
