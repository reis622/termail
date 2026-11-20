package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

type browserReq struct {
	key bodyKey
	env backend.Envelope
}

type openBrowserDone struct {
	path   string
	err    error
	ai     int
	folder string
	env    backend.Envelope
}

// openInBrowser writes the selected mail to a temp HTML file and opens it
// in the default browser. The envelope is the one captured before the list
// changes; opening marks it read the same way Enter does.
func (m *Model) openInBrowser(ai int, folder string, e backend.Envelope) tea.Cmd {
	// Capture the envelope/UID first; mark-read waits until the browser
	// actually opens so a failed ShellExecute / xdg-open does not pull
	// the mail out of Gelen Kutusu or show a success toast.
	k := bodyKey{ai, folder, e.UID}
	env := e
	m.clampCursor()
	if p := m.body(k); p == nil {
		if p = m.bodyFromDisk(k); p != nil {
			m.bodies.Put(k, p)
		}
	}
	if p := m.body(k); p != nil {
		// Body already here: write the HTML now and only ShellExecute
		// asynchronously — the 12:18 behaviour that opened immediately.
		return m.openBrowserNow(ai, folder, env, p)
	}
	m.wantBrowser = &browserReq{key: k, env: env}
	m.toasts.Push(toast.Info, "", "Mail yükleniyor, tarayıcıda açılacak...", m.opt.Now(), 8*time.Second)
	load := m.loadBody(k)
	if load == nil && !m.loading[k] && m.body(k) == nil {
		m.wantBrowser = nil
		m.toasts.Push(toast.Error, "", "İleti yüklenemedi, tarayıcıda açılamadı.", m.opt.Now(), 6*time.Second)
		return nil
	}
	return load
}

// openBrowserNow writes the temp HTML on the UI goroutine (same as the
// original o path) and returns a command that only hands the file to the
// default program.
func (m *Model) openBrowserNow(ai int, folder string, e backend.Envelope, p *mailmsg.Parsed) tea.Cmd {
	path, err := writeMailHTML(e, p)
	if err != nil {
		m.toasts.Push(toast.Error, "", "Tarayıcıda açılamadı: "+friendly(err), m.opt.Now(), 6*time.Second)
		return nil
	}
	return func() tea.Msg {
		return openBrowserDone{path: path, err: openFileFn(path), ai: ai, folder: folder, env: e}
	}
}

func (m *Model) onOpenBrowserDone(msg openBrowserDone) tea.Cmd {
	if msg.err != nil {
		m.logErr("browser", msg.err)
		m.toasts.Push(toast.Error, "", "Tarayıcıda açılamadı: "+friendly(msg.err), m.opt.Now(), 6*time.Second)
		m.clampCursor()
		return nil
	}
	m.toastInfo(toast.Info, "", "Tarayıcıda açılıyor")
	env := msg.env
	env.Seen = true
	cmd := m.markRead(msg.ai, msg.folder, msg.env.UID)
	m.followOpened(env, false)
	m.clampCursor()
	return cmd
}

func (m *Model) flushWantBrowser(k bodyKey, p *mailmsg.Parsed, err error) tea.Cmd {
	if m.wantBrowser == nil || m.wantBrowser.key != k {
		return nil
	}
	req := m.wantBrowser
	m.wantBrowser = nil
	if err != nil || p == nil {
		msg := "İleti yüklenemedi, tarayıcıda açılamadı."
		if err != nil {
			msg = "İleti yüklenemedi, tarayıcıda açılamadı: " + friendly(err)
		}
		m.toasts.Push(toast.Error, "", msg, m.opt.Now(), 6*time.Second)
		m.clampCursor()
		return nil
	}
	return m.openBrowserNow(req.key.acct, req.key.folder, req.env, p)
}

func writeMailHTML(e backend.Envelope, p *mailmsg.Parsed) (string, error) {
	dir := filepath.Join(os.TempDir(), "Termail")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.html", e.UID))
	return path, os.WriteFile(path, []byte(mailmsg.BrowserHTML(p)), 0o600)
}
