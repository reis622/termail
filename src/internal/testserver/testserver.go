// Package testserver runs a local, in-memory mail server pair (IMAP +
// SMTP) for automated tests and manual end-to-end checks. Never used by
// the real client.
package testserver

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// Server is a running IMAP+SMTP pair.
type Server struct {
	IMAPAddr, SMTPAddr string
	mem                *imapmemserver.Server
	imap               *imapserver.Server
	smtp               *smtp.Server
	mu                 sync.Mutex
	users              map[string]*imapmemserver.User
	pass               map[string]string
	Received           []Received
	SMTPConns          int // SMTP connections accepted (tests of reuse)
}

// Received is one message accepted by the SMTP side.
type Received struct {
	From string
	To   []string
	Data []byte
}

// Start listens on the given addresses ("127.0.0.1:0" for random ports).
func Start(imapAddr, smtpAddr string) (*Server, error) {
	s := &Server{mem: imapmemserver.New(), users: map[string]*imapmemserver.User{}, pass: map[string]string{}}
	s.imap = imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return s.mem.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}, imap.CapIdle: {}, imap.CapMove: {}, imap.CapUIDPlus: {}},
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	il, err := net.Listen("tcp", imapAddr)
	if err != nil {
		return nil, err
	}
	s.IMAPAddr = il.Addr().String()
	go s.imap.Serve(il)

	s.smtp = smtp.NewServer(smtp.BackendFunc(func(c *smtp.Conn) (smtp.Session, error) {
		s.mu.Lock()
		s.SMTPConns++
		s.mu.Unlock()
		return &sess{s: s}, nil
	}))
	s.smtp.AllowInsecureAuth = true
	s.smtp.Domain = "localhost"
	s.smtp.ReadTimeout = 30 * time.Second
	s.smtp.WriteTimeout = 30 * time.Second
	sl, err := net.Listen("tcp", smtpAddr)
	if err != nil {
		il.Close()
		return nil, err
	}
	s.SMTPAddr = sl.Addr().String()
	go s.smtp.Serve(sl)
	return s, nil
}

// AddUser creates a mailbox owner with the usual folders.
func (s *Server) AddUser(email, password string) *imapmemserver.User {
	u := imapmemserver.NewUser(email, password)
	for _, f := range []string{"INBOX", "Sent", "Drafts", "Trash", "Archive", "Projeler"} {
		u.Create(f, nil)
	}
	s.mem.AddUser(u)
	s.mu.Lock()
	s.users[strings.ToLower(email)] = u
	s.pass[strings.ToLower(email)] = password
	s.mu.Unlock()
	return u
}

// Deliver appends a raw message to a user's folder (like a new incoming mail).
func (s *Server) Deliver(email, folder string, raw []byte) error {
	return s.DeliverAt(email, folder, raw, time.Now())
}

// DeliverAt is Deliver with a chosen internal date (arrival time).
func (s *Server) DeliverAt(email, folder string, raw []byte, at time.Time) error {
	s.mu.Lock()
	u := s.users[strings.ToLower(email)]
	s.mu.Unlock()
	if u == nil {
		return errors.New("no such user")
	}
	_, err := u.Append(folder, literal{bytes.NewReader(raw), int64(len(raw))}, &imap.AppendOptions{Time: at})
	return err
}

type literal struct {
	*bytes.Reader
	n int64
}

func (l literal) Size() int64 { return l.n }

// Close stops both servers.
func (s *Server) Close() {
	s.imap.Close()
	s.smtp.Close()
}

type sess struct {
	s    *Server
	from string
	to   []string
	user string
}

func (x *sess) AuthMechanisms() []string { return []string{"PLAIN"} }

func (x *sess) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, username, password string) error {
		x.s.mu.Lock()
		defer x.s.mu.Unlock()
		if p, ok := x.s.pass[strings.ToLower(username)]; !ok || p != password {
			return errors.New("invalid credentials")
		}
		x.user = username
		return nil
	}), nil
}

func (x *sess) Mail(from string, _ *smtp.MailOptions) error {
	if x.user == "" {
		return errors.New("auth required")
	}
	x.from = from
	return nil
}
func (x *sess) Rcpt(to string, _ *smtp.RcptOptions) error { x.to = append(x.to, to); return nil }
func (x *sess) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	x.s.mu.Lock()
	x.s.Received = append(x.s.Received, Received{x.from, append([]string(nil), x.to...), b})
	x.s.mu.Unlock()
	for _, rc := range x.to {
		_ = x.s.Deliver(rc, "INBOX", b) // local recipients get it in their inbox
	}
	return nil
}
func (x *sess) Reset()        { x.from, x.to = "", nil }
func (x *sess) Logout() error { return nil }

// LastReceived returns the most recent SMTP message.
func (s *Server) LastReceived() (Received, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Received) == 0 {
		return Received{}, false
	}
	return s.Received[len(s.Received)-1], true
}

// Conns returns the number of SMTP connections so far.
func (s *Server) Conns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SMTPConns
}
