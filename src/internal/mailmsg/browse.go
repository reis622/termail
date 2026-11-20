package mailmsg

import (
	"encoding/base64"
	"html"
	"regexp"
	"strings"
	"termail/internal/i18n"
)

// BrowserHTML is a self-contained HTML page for opening a mail in a
// browser: a small From/To/Date/Subject header and either the original
// HTML body (cid: images inlined as data URIs when the attachment is
// at hand) or the plain text, escaped, in a <pre>.
func BrowserHTML(p *Parsed) string {
	if p == nil {
		return ""
	}
	hdr := browserHeader(p)
	body := strings.TrimSpace(p.HTML)
	if body != "" {
		body = inlineCID(body, p.Attachments)
	} else {
		body = "<pre style=\"white-space:pre-wrap;font-family:system-ui,sans-serif\">" + html.EscapeString(p.Text) + "</pre>"
	}
	low := strings.ToLower(body)
	if i := strings.Index(low, "<body"); i >= 0 {
		if j := strings.Index(low[i:], ">"); j >= 0 {
			at := i + j + 1
			return body[:at] + hdr + body[at:]
		}
	}
	title := html.EscapeString(p.Subject)
	return `<!DOCTYPE html><html><head><meta charset="utf-8"><title>` + title + `</title></head><body>` + hdr + body + `</body></html>`
}

func browserHeader(p *Parsed) string {
	row := func(k, v string) string {
		if strings.TrimSpace(v) == "" {
			return ""
		}
		return `<tr><th style="text-align:left;color:#666;font-weight:normal;padding:2px 12px 2px 0;vertical-align:top;white-space:nowrap">` + k +
			`</th><td style="padding:2px 0">` + html.EscapeString(v) + `</td></tr>`
	}
	from, to := "", ""
	for i, a := range p.From {
		if i > 0 {
			from += ", "
		}
		from += Display(a)
	}
	for i, a := range p.To {
		if i > 0 {
			to += ", "
		}
		to += Display(a)
	}
	date := ""
	if !p.Date.IsZero() {
		date = LongDate(p.Date)
	}
	return `<header style="font-family:system-ui,sans-serif;border-bottom:1px solid #ccc;margin:0 0 1.25em;padding:12px 0;color:#222">` +
		`<table style="border-collapse:collapse;font-size:14px">` +
		row(i18n.T("Kimden"), from) + row(i18n.T("Kime"), to) + row(i18n.T("Tarih"), date) + row(i18n.T("Konu"), p.Subject) +
		`</table></header>`
}

func inlineCID(doc string, atts []Attachment) string {
	for _, a := range atts {
		if a.ContentID == "" || len(a.Data) == 0 {
			continue
		}
		ct := strings.ToLower(a.ContentType)
		if i := strings.IndexByte(ct, ';'); i >= 0 {
			ct = strings.TrimSpace(ct[:i])
		}
		if !strings.HasPrefix(ct, "image/") {
			continue
		}
		data := "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(a.Data)
		re := regexp.MustCompile(`(?i)cid:` + regexp.QuoteMeta(a.ContentID))
		doc = re.ReplaceAllString(doc, data)
	}
	return doc
}
