package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/htmltext"
	"termail/internal/mailmsg"
)

func tabMail() *mailmsg.Parsed {
	mk := string(htmltext.MarkRune)
	return &mailmsg.Parsed{
		Text: "x",
		View: "Merhaba.\nToplantıya katılmak için tıkla " + mk + "[1].\nŞirket duyurusu: https://www.firma.com/d " + mk + "[2]\nÖdemeyi tamamla " + mk + "[3].",
		Links: []htmltext.Link{
			{URL: "https://meet.firma.com/x", Text: "https://meet.firma.com/x"},
			{URL: "https://www.firma.com/d", Text: "https://www.firma.com/d"},
			{URL: "https://odeme-guvenlik.xyz/pay", Text: "www.bankam.com.tr"},
		},
		Attachments: []mailmsg.Attachment{
			{Name: "notlar.pdf", ContentType: "application/pdf", Size: 84000},
			{Name: "Bütçe 2026.csv", ContentType: "text/csv", Data: []byte("Kalem;Plan;Gerçek\nYazılım;12.000;11.450\nOfis;3.200;3.150\n"), Size: 12000},
			{Name: "kurulum.exe", Data: []byte("MZ"), Size: 2},
		},
	}
}

func TestAttachmentsTab(t *testing.T) {
	m := rankModel(t)
	m.bodies.Put(bodyKey{0, "INBOX", 2}, tabMail())
	selectUID(t, m, 2)
	press(m, "enter")
	if m.focus != focusReader || m.pv.attMode {
		t.Fatal("reader not open on the Mail tab")
	}
	press(m, "e")
	if !m.pv.attMode || m.pv.sel != 0 {
		t.Fatal("e should show Ekler ve Bağlantılar")
	}
	v := m.View()
	for _, want := range []string{"Ekler ve Bağlantılar (3 · 3)", "PDF", "Tablo", "Dosya", "Bağlantılar", "Toplantıya katılmak için tıkla", "meet.firma.com", "Dikkat: farklı adrese gidiyor", "odeme-guvenlik.xyz"} {
		if !strings.Contains(v, want) {
			t.Fatalf("tab lacks %q", want)
		}
	}
	cur := m.cur
	press(m, "down") // the CSV: aligned preview
	if m.pv.sel != 1 || m.pv.att != 1 {
		t.Fatalf("↓: sel %d", m.pv.sel)
	}
	v = m.View()
	if !strings.Contains(v, "Önizleme") || !strings.Contains(v, "Yazılım   12.000   11.450") {
		t.Fatalf("csv preview missing:\n%s", v)
	}
	press(m, "2") // link [2], no account switch
	if m.pv.sel != 4 || m.cur != cur {
		t.Fatalf("2: sel %d account %d", m.pv.sel, m.cur)
	}
	var opened []string
	old := openURLFn
	openURLFn = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURLFn = old }()
	press(m, "enter")
	if len(opened) != 1 || opened[0] != "https://www.firma.com/d" {
		t.Fatalf("Enter on a link: %v", opened)
	}
	// a program file needs an e
	dir := t.TempDir()
	oldDir, oldOpen := attTempDir, openFileFn
	var files []string
	attTempDir = func() string { return dir }
	openFileFn = func(p string) error { files = append(files, p); return nil }
	defer func() { attTempDir, openFileFn = oldDir, oldOpen }()
	press(m, "up")
	press(m, "up") // link 1, then kurulum.exe
	if m.pv.att != 2 {
		t.Fatalf("att %d", m.pv.att)
	}
	cmd := m.onKey(key("enter"))
	if cmd != nil || m.confirm == nil || !strings.Contains(m.confirm.text, "program dosyası") {
		t.Fatal("exe opened without asking")
	}
	press(m, "enter") // Enter is not yes
	if m.confirm != nil || len(files) != 0 {
		t.Fatal("Enter must not confirm")
	}
	m.onKey(key("enter"))
	cmd = m.onKey(key("e"))
	m.Update(cmd())
	if len(files) != 1 || filepath.Dir(files[0]) != dir {
		t.Fatalf("exe after e: %v", files)
	}
	if b, _ := os.ReadFile(files[0]); string(b) != "MZ" {
		t.Fatal("temp copy wrong")
	}
	press(m, "esc")
	if m.pv.attMode || m.focus != focusReader {
		t.Fatal("Esc returns to the Mail tab")
	}
	press(m, "esc")
	m.Update(tea.KeyMsg{Type: tea.KeyF3})
	if m.cur != 1 {
		t.Fatal("F3 outside the tab should switch account")
	}
}

func TestAttachmentsTabKeysAndClick(t *testing.T) {
	m := rankModel(t)
	m.bodies.Put(bodyKey{0, "INBOX", 2}, tabMail())
	selectUID(t, m, 2)
	press(m, "enter")
	m.View()
	var tab hit
	for _, x := range m.hits.list {
		if x.kind == hitReaderTab && x.idx == 1 {
			tab = x
		}
	}
	if tab.w == 0 {
		t.Fatal("no tab hit")
	}
	click(m, tab.x+2, tab.y)
	if !m.pv.attMode {
		t.Fatal("click on the tab should open it")
	}
	m.View()
	var row hit
	for _, x := range m.hits.list {
		if x.kind == hitTabItem && x.idx == 3 {
			row = x
		}
	}
	click(m, row.x+2, row.y)
	if m.pv.sel != 3 {
		t.Fatalf("click on a link row: sel %d", m.pv.sel)
	}
	p, _ := m.bodies.Peek(bodyKey{0, "INBOX", 2})
	k := readerKeys(p, true, true)
	if k[0].key != "Enter" || k[1].key != "s" || k[1].label != "Kaydet" || k[2].key != "Esc" {
		t.Fatalf("tab keybar %+v", k)
	}
	found := false
	for _, h := range readerKeys(p, true, false) {
		found = found || h.key == "e" && h.label == "Ekler"
	}
	if !found {
		t.Fatal("e missing from the Mail keybar")
	}
}

func TestAttachmentsTabNothing(t *testing.T) {
	m := rankModel(t)
	selectUID(t, m, 3) // no attachments, no links
	press(m, "enter")
	press(m, "e")
	if m.pv.attMode || !hasToast(m, "Bu iletide ek ya da bağlantı yok") {
		t.Fatal("nothing to show: toast, Mail tab stays")
	}
}

func TestLinkLabelsAndPreviews(t *testing.T) {
	p := tabMail()
	if got := linkLabel(p, 0); got != "Toplantıya katılmak için tıkla" {
		t.Fatalf("label 1 %q", got)
	}
	if got := linkLabel(p, 1); got != "Şirket duyurusu" {
		t.Fatalf("label 2 %q", got)
	}
	if got := linkLabel(p, 2); got != "www.bankam.com.tr" {
		t.Fatalf("label 3 %q", got)
	}
	if l := attPreview(p.Attachments[0], 40, 10); len(l) != 1 || !strings.Contains(l[0].text, "PDF önizlemesi yok") {
		t.Fatalf("pdf %+v", l)
	}
	j := attPreview(mailmsg.Attachment{Name: "a.json", Data: []byte(`{"a":1}`)}, 40, 10)
	if len(j) != 3 || j[1].text != `  "a": 1` {
		t.Fatalf("json %+v", j)
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	for x := 0; x < 20; x++ {
		for y := 0; y < 10; y++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	pic := attPreview(mailmsg.Attachment{Name: "a.png", Data: buf.Bytes()}, 20, 10)
	if len(pic) != 5 || len(pic[0].px) != 20 || pic[0].px[0][0] != "#ff0000" {
		t.Fatalf("image %d rows", len(pic))
	}
	if !riskyAttachment("fatura.pdf.exe") || riskyAttachment("fatura.pdf") || !riskyAttachment("x.JS") {
		t.Fatal("risky ext")
	}
}

func TestAttachmentsTabEasyNavigation(t *testing.T) {
	m := rankModel(t)
	p := tabMail()
	for i := 0; i < 20; i++ { // many links: the list must scroll with the selection
		p.Links = append(p.Links, htmltext.Link{URL: "https://site" + string(rune('a'+i)) + ".com/", Text: "Bağlantı " + string(rune('A'+i))})
	}
	m.bodies.Put(bodyKey{0, "INBOX", 2}, p)
	selectUID(t, m, 2)
	press(m, "enter")
	press(m, "e")
	n := len(tabItems(p))
	press(m, "up") // stops at the top
	if m.pv.sel != 0 {
		t.Fatal("↑ on the first row should stay")
	}
	press(m, "end")
	if m.pv.sel != n-1 {
		t.Fatalf("End: %d", m.pv.sel)
	}
	press(m, "down") // stops at the bottom
	if m.pv.sel != n-1 {
		t.Fatal("↓ on the last row should stay")
	}
	v := m.View()
	if !strings.Contains(v, "Bağlantı T") { // the last one is on screen
		t.Fatal("selection scrolled out of view")
	}
	press(m, "home")
	if m.pv.sel != 0 || !strings.Contains(m.View(), "notlar.pdf") {
		t.Fatal("Home")
	}
	press(m, "5")
	if m.pv.sel != len(p.Attachments)+4 {
		t.Fatalf("5: %d", m.pv.sel)
	}
	// the wheel moves the selection
	var row hit
	for _, x := range m.hits.list {
		if x.kind == hitTabItem {
			row = x
		}
	}
	before := m.pv.sel
	wheel(m, row.x+2, row.y, true)
	if m.pv.sel != before+1 {
		t.Fatalf("wheel: %d → %d", before, m.pv.sel)
	}
	// double click opens
	var opened []string
	old := openURLFn
	openURLFn = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURLFn = old }()
	m.View()
	for _, x := range m.hits.list {
		if x.kind == hitTabItem && x.idx == len(p.Attachments) { // link [1]
			row = x
		}
	}
	click(m, row.x+2, row.y)
	if m.pv.sel != len(p.Attachments) || len(opened) != 0 {
		t.Fatal("one click selects only")
	}
	click(m, row.x+2, row.y)
	if len(opened) != 1 || opened[0] != p.Links[0].URL {
		t.Fatalf("double click: %v", opened)
	}
	// the key bar names the keys
	bar := m.View()
	for _, k := range []string{"Aç", "Kaydet", "Mail"} {
		if !strings.Contains(bar, k) {
			t.Fatalf("key bar lacks %q", k)
		}
	}
}
