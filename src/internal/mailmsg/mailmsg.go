// Package mailmsg parses incoming MIME messages (text, HTML -> text,
// attachments) and builds outgoing ones.
package mailmsg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset" // Turkish & other legacy charsets
	"github.com/emersion/go-message/mail"

	"termail/internal/htmltext"
)

// Attachment is a file inside a message.
type Attachment struct {
	Name        string
	ContentType string
	Size        int
	Data        []byte
	ContentID   string // Content-ID without <>, for cid: in HTML
}

// Parsed is a decoded message ready for display.
type Parsed struct {
	From        []*mail.Address
	To          []*mail.Address
	Cc          []*mail.Address
	ReplyTo     []*mail.Address
	Subject     string
	Date        time.Time
	MessageID   string
	References  []string
	Text        string // plain text (HTML already converted)
	HTML        string // original HTML body, if the message had one
	FromHTML    bool
	Attachments []Attachment
	// View is Text for the reader: links keep their text and get a small
	// numbered marker (htmltext.Marker); Links holds them by number.
	View  string
	Links []htmltext.Link
}

const maxPart = 50 << 20

// ErrUnreadable means the message could not be decoded at all. The UI then
// says so in plain words instead of ever showing the raw source.
var ErrUnreadable = errors.New("ileti çözülemedi")

// Parse decodes a raw RFC 5322 message: walks every (nested) part, decodes
// transfer encodings and charsets, picks text/plain (or HTML converted to
// text) as the body and keeps everything else as attachments. Raw headers
// are never part of Text.
func Parse(raw []byte) (*Parsed, error) {
	return parse(raw, 0)
}

func parse(raw []byte, depth int) (*Parsed, error) {
	raw = bytes.TrimLeft(raw, "\r\n")
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if mr == nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if !mr.Header.Fields().Next() {
		return nil, ErrUnreadable
	}
	p := &Parsed{}
	h := mr.Header
	p.Subject, _ = h.Subject()
	p.From, _ = h.AddressList("From")
	p.To, _ = h.AddressList("To")
	p.Cc, _ = h.AddressList("Cc")
	p.ReplyTo, _ = h.AddressList("Reply-To")
	p.Date, _ = h.Date()
	p.MessageID, _ = h.MessageID()
	p.References, _ = h.MsgIDList("References")
	if len(p.References) == 0 {
		p.References, _ = h.MsgIDList("In-Reply-To")
	}
	var plain, html []string
	var inner []*Parsed // attached messages (forwards)
	addAtt := func(name, ct, cid string, body []byte) {
		if name == "" {
			name = defaultName(ct, len(p.Attachments)+1)
		}
		p.Attachments = append(p.Attachments, Attachment{Name: decodeName(name), ContentType: ct, Size: len(body), Data: body, ContentID: cid})
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && part == nil {
			break // keep what we could read
		}
		body, _ := io.ReadAll(io.LimitReader(part.Body, maxPart))
		var ct, name, disp, cid string
		if g, ok := part.Header.(interface{ Get(string) string }); ok {
			cid = strings.Trim(strings.TrimSpace(g.Get("Content-ID")), "<>")
		}
		switch ph := part.Header.(type) {
		case *mail.InlineHeader:
			var params, dparams map[string]string
			ct, params, _ = ph.ContentType()
			disp, dparams, _ = ph.ContentDisposition()
			name = dparams["filename"]
			if name == "" {
				name = params["name"]
			}
		case *mail.AttachmentHeader:
			ct, _, _ = ph.ContentType()
			name, _ = ph.Filename()
			disp = "attachment"
		default:
			continue
		}
		ct = strings.ToLower(ct)
		switch {
		case (ct == "text/plain" || ct == "" || ct == "text/html" || ct == "application/xhtml+xml") && name == "" && disp != "attachment":
			if ct == "text/html" || ct == "application/xhtml+xml" {
				html = append(html, string(body))
			} else {
				plain = append(plain, string(body))
			}
		case ct == "message/rfc822" || ct == "message/global":
			if depth < 3 {
				if ip, err := parse(body, depth+1); err == nil {
					inner = append(inner, ip)
				}
			}
			if name == "" {
				name = "ileti.eml"
			}
			addAtt(name, ct, cid, body)
		case ct == "text/rfc822-headers" || ct == "message/delivery-status" || ct == "message/global-delivery-status" || ct == "message/global-headers":
			if name == "" {
				name = "teslim-raporu.txt"
				if strings.Contains(ct, "headers") {
					name = "ozgun-ileti-bilgileri.txt"
				}
			}
			addAtt(name, ct, cid, body)
		default:
			addAtt(name, ct, cid, body)
		}
	}
	// "text/plain" parts (or parts with no type) that are really HTML: use
	// the HTML converter; when a real HTML part exists, that one wins.
	var realPlain, sniffed []string
	for _, b := range plain {
		if LooksHTML(b) {
			sniffed = append(sniffed, b)
		} else {
			realPlain = append(realPlain, b)
		}
	}
	plain = realPlain
	if len(html) == 0 {
		html = sniffed
	}
	if len(html) > 0 {
		p.HTML = strings.Join(html, "\n")
	}
	text := strings.TrimSpace(htmltext.CleanText(strings.Join(plain, "\n\n")))
	if len(html) > 0 {
		htmlText := htmltext.Convert(strings.Join(html, "\n"))
		if preferHTMLBody(text, htmlText) {
			text = htmlText
			p.FromHTML = true
		}
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	stripped := false
	if depth < 3 {
		t2 := stripHeaderBlock(text, depth)
		stripped = t2 != text
		text = t2
	}
	defer func() {
		var set htmltext.Links
		if p.FromHTML && !stripped && len(inner) == 0 {
			v := htmltext.ConvertLinks(strings.Join(html, "\n"), &set)
			v = strings.ReplaceAll(strings.ReplaceAll(v, "\r\n", "\n"), "\r", "\n")
			p.View = strings.TrimRight(v, "\n ")
		} else {
			p.View = htmltext.Linkify(strings.ReplaceAll(p.Text, string(htmltext.MarkRune), ""), &set)
		}
		p.Links = set.List
	}()
	for _, ip := range inner {
		if strings.TrimSpace(ip.Text) == "" {
			continue
		}
		var sb strings.Builder
		if strings.TrimSpace(text) != "" {
			sb.WriteString(text + "\n\n")
		}
		sb.WriteString("──── İletilen ileti ────\n")
		if len(ip.From) > 0 {
			sb.WriteString("Kimden: " + Display(ip.From[0]) + "\n")
		}
		if ip.Subject != "" {
			sb.WriteString("Konu: " + ip.Subject + "\n")
		}
		sb.WriteString("\n" + ip.Text)
		text = sb.String()
	}
	p.Text = strings.TrimRight(text, "\n ")
	return p, nil
}

// traceHeaders are header names that only ever appear in a message's
// header block, never at the start of a human-written text.
var traceHeaders = []string{"received:", "dkim-signature:", "arc-seal:", "arc-message-signature:", "arc-authentication-results:",
	"x-google-dkim-signature:", "authentication-results:", "return-path:", "delivered-to:", "x-received:", "received-spf:", "x-gm-message-state:"}

// stripHeaderBlock is a safety net for broken senders whose text part
// itself starts with a raw header block: such a block is parsed as a
// message when possible, otherwise dropped up to the first blank line.
func stripHeaderBlock(text string, depth int) string {
	head := strings.ToLower(strings.TrimLeft(text, " \n"))
	isTrace := false
	for _, t := range traceHeaders {
		if strings.HasPrefix(head, t) {
			isTrace = true
			break
		}
	}
	if !isTrace {
		return text
	}
	if ip, err := parse([]byte(strings.TrimLeft(text, " \n")), depth+1); err == nil && strings.TrimSpace(ip.Text) != "" {
		return ip.Text
	}
	t := strings.TrimLeft(text, " \n")
	if i := strings.Index(t, "\n\n"); i >= 0 {
		return strings.TrimLeft(t[i+2:], "\n")
	}
	return ""
}

func decodeName(n string) string {
	dec := new(mime.WordDecoder)
	if d, err := dec.DecodeHeader(n); err == nil {
		return d
	}
	return n
}

func defaultName(ct string, i int) string {
	ext := ""
	if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
		ext = exts[0]
	}
	return fmt.Sprintf("ek-%d%s", i, ext)
}

// Outgoing is a message to send.
type Outgoing struct {
	From        *mail.Address
	To, Cc      []*mail.Address
	Subject     string
	Body        string
	InReplyTo   string
	References  []string
	Attachments []Attachment
	Date        time.Time
}

// Recipients returns all envelope recipients (To + Cc).
func (o *Outgoing) Recipients() []string {
	var r []string
	for _, a := range append(append([]*mail.Address{}, o.To...), o.Cc...) {
		r = append(r, a.Address)
	}
	return r
}

// Build serializes the message as MIME (multipart/mixed when it has attachments).
func Build(o *Outgoing) ([]byte, error) {
	if o.From == nil {
		return nil, errors.New("gönderen yok")
	}
	var h mail.Header
	d := o.Date
	if d.IsZero() {
		d = time.Now()
	}
	h.SetDate(d)
	h.SetAddressList("From", []*mail.Address{o.From})
	h.SetAddressList("To", o.To)
	if len(o.Cc) > 0 {
		h.SetAddressList("Cc", o.Cc)
	}
	h.Set("Subject", encodeHeaderText(o.Subject))
	dom := "termail.local"
	if i := strings.LastIndex(o.From.Address, "@"); i >= 0 {
		dom = o.From.Address[i+1:]
	}
	if err := h.GenerateMessageIDWithHostname(dom); err != nil {
		return nil, err
	}
	if o.InReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{o.InReplyTo})
	}
	if len(o.References) > 0 {
		h.SetMsgIDList("References", o.References)
	}
	h.Set("User-Agent", "Termail")
	body := strings.ReplaceAll(o.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")

	var buf bytes.Buffer
	if len(o.Attachments) == 0 {
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := mail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		io.WriteString(w, body)
		if err := w.Close(); err != nil {
			return nil, err
		}
		return finishRaw(buf.Bytes()), nil
	}
	mw, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	var th mail.InlineHeader
	th.Set("Content-Type", "text/plain; charset=utf-8")
	th.Set("Content-Transfer-Encoding", "quoted-printable")
	tw, err := mw.CreateSingleInline(th)
	if err != nil {
		return nil, err
	}
	io.WriteString(tw, body)
	tw.Close()
	for _, a := range o.Attachments {
		var ah mail.AttachmentHeader
		ct := a.ContentType
		if ct == "" {
			ct = TypeByName(a.Name)
		}
		ah.Set("Content-Type", ct)
		ah.Set("Content-Transfer-Encoding", "base64")
		ah.Set("Content-Disposition", dispositionFor(a.Name))
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(a.Data); err != nil {
			return nil, err
		}
		aw.Close()
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return finishRaw(buf.Bytes()), nil
}

// TypeByName guesses a content type from a file name.
func TypeByName(name string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		return t
	}
	return "application/octet-stream"
}

// ParseAddresses accepts "a@b.com; Ali <c@d.com>, e@f" lists.
func ParseAddresses(s string) ([]*mail.Address, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ";", ","))
	s = strings.Trim(s, ", ")
	if s == "" {
		return nil, nil
	}
	return mail.ParseAddressList(s)
}

// FormatAddresses renders a list for display/editing.
func FormatAddresses(l []*mail.Address) string {
	var parts []string
	for _, a := range l {
		if a.Name != "" {
			parts = append(parts, a.Name+" <"+a.Address+">")
		} else {
			parts = append(parts, a.Address)
		}
	}
	return strings.Join(parts, ", ")
}

// Display returns "Name" or the address.
func Display(a *mail.Address) string {
	if a == nil {
		return ""
	}
	if a.Name != "" {
		return a.Name
	}
	return a.Address
}

var aylar = []string{"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}
var kisaAylar = []string{"Oca", "Şub", "Mar", "Nis", "May", "Haz", "Tem", "Ağu", "Eyl", "Eki", "Kas", "Ara"}

// LongDate: "14 Kasım 2026, 13:05".
func LongDate(t time.Time) string {
	t = t.Local()
	return fmt.Sprintf("%d %s %d, %02d:%02d", t.Day(), aylar[t.Month()-1], t.Year(), t.Hour(), t.Minute())
}

// ShortDate: "14:31" today, "Dün" yesterday, "12 Kas" otherwise ("12.11.2025" other years).
func ShortDate(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
	}
	yest := now.AddDate(0, 0, -1)
	if y, m, d := yest.Date(); y == y1 && m == m1 && d == d1 {
		return "Dün"
	}
	if y1 == y2 {
		return fmt.Sprintf("%d %s", d1, kisaAylar[m1-1])
	}
	return fmt.Sprintf("%02d.%02d.%d", d1, int(m1), y1)
}

// HumanSize: "212 KB", "1,4 MB".
func HumanSize(n int) string {
	switch {
	case n >= 1<<20:
		return strings.Replace(fmt.Sprintf("%.1f MB", float64(n)/(1<<20)), ".", ",", 1)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", (n+512)>>10)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// ReplySubject adds "Re: " once.
func ReplySubject(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(l, "re:") || strings.HasPrefix(l, "ynt:") {
		return s
	}
	return "Re: " + s
}

// ForwardSubject adds "Fwd: " once.
func ForwardSubject(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(l, "fwd:") || strings.HasPrefix(l, "fw:") || strings.HasPrefix(l, "ilt:") {
		return s
	}
	return "Fwd: " + s
}

// QuoteHeader: "Ayşe Demir, 14 Kasım 2026 14:30 tarihinde yazdı:"
func QuoteHeader(p *Parsed) string {
	who := ""
	if len(p.From) > 0 {
		who = Display(p.From[0])
	}
	return fmt.Sprintf("%s, %s tarihinde yazdı:", who, strings.Replace(LongDate(p.Date), ",", "", 1))
}

// QuoteLines prefixes every line with "> ".
func QuoteLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, ">") {
			out = append(out, ">"+l)
		} else {
			out = append(out, "> "+l)
		}
	}
	return out
}

// ShortDay formats a date as "14 Kas 2026".
func ShortDay(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	return fmt.Sprintf("%d %s %d", t.Day(), kisaAylar[t.Month()-1], t.Year())
}

var htmlTagRe = regexp.MustCompile(`(?i)</?(html|head|body|meta|style|div|span|table|tbody|thead|tr|td|th|p|br|a|img|font|b|i|u|strong|em|center|ul|ol|li|h[1-6]|hr|title|link|section|header|footer)\b[^<>]{0,1000}>`)

// LooksHTML sniffs HTML in a part labelled as plain text (or unlabelled):
// it starts with an HTML opening tag, or tags make up much of the text.
func LooksHTML(s string) bool {
	t := strings.TrimLeft(s, "\ufeff \t\r\n")
	head := t
	if len(head) > 64 {
		head = head[:64]
	}
	head = strings.ToLower(head)
	for _, p := range []string{"<!doctype", "<html", "<head", "<body", "<table", "<div", "<meta", "<style", "<?xml", "<!--[if"} {
		if strings.HasPrefix(head, p) {
			return true
		}
	}
	if len(t) < 40 {
		return false
	}
	sample := t
	if len(sample) > 64<<10 {
		sample = sample[:64<<10]
	}
	tags := htmlTagRe.FindAllStringIndex(sample, -1)
	if len(tags) < 6 {
		return false
	}
	n := 0
	for _, m := range tags {
		n += m[1] - m[0]
	}
	return n*5 >= len(sample) // tags are at least a fifth of the text
}

// preferHTMLBody reports whether the HTML part should be shown instead of
// the text/plain part: the plain body is empty, whitespace, a placeholder
// such as TEXT_CONTENT, or very short next to a much richer HTML part.
func preferHTMLBody(plain, htmlText string) bool {
	p := strings.TrimSpace(plain)
	h := strings.TrimSpace(htmlText)
	if h == "" {
		return false
	}
	if p == "" || isPlainPlaceholder(p) {
		return true
	}
	pr, hr := []rune(p), []rune(h)
	return len(pr) <= 40 && len(hr) >= 80 && len(hr) >= 4*len(pr)
}

// isPlainPlaceholder reports newsletter-style stubs that are not real
// message text (e.g. "TEXT_CONTENT" used as the text/plain part).
func isPlainPlaceholder(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	key := strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(t, "-", " ")), "_"))
	key = strings.ReplaceAll(key, "-", "_")
	switch key {
	case "text_content", "textcontent", "html_content", "htmlcontent",
		"body_content", "bodycontent", "message_content",
		"this_is_an_html_email", "please_view_this_email_in_html",
		"view_this_email_in_html":
		return true
	}
	// a single ALL_CAPS_TOKEN with underscores and no spaces
	if !strings.ContainsAny(t, " \t\n") && len(t) >= 8 && len(t) <= 40 {
		ok := true
		for _, r := range t {
			if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
