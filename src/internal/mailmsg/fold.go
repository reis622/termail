package mailmsg

import (
	"bytes"
	"mime"
	"net/url"
	"strings"
)

// finishRaw makes the built message strictly conformant: every header line
// (top level and inside MIME parts) is folded to at most 76 characters at
// whitespace, and the message ends with CRLF.
func finishRaw(raw []byte) []byte {
	lines := strings.Split(string(raw), "\r\n")
	var out []string
	inHeader := true
	var boundaries []string
	var cur []string // logical header line being collected
	flush := func() {
		if len(cur) == 0 {
			return
		}
		logical := strings.Join(cur, " ")
		cur = nil
		if k, v, ok := strings.Cut(logical, ":"); ok && strings.EqualFold(k, "Content-Type") {
			if _, ps, err := mime.ParseMediaType(strings.TrimSpace(v)); err == nil && ps["boundary"] != "" {
				boundaries = append(boundaries, ps["boundary"])
			}
		}
		out = append(out, fold(logical, 76)...)
	}
	for i, l := range lines {
		if inHeader {
			if l == "" {
				flush()
				out = append(out, "")
				inHeader = false
				continue
			}
			if (l[0] == ' ' || l[0] == '\t') && len(cur) > 0 {
				cur = append(cur, strings.TrimSpace(l))
				continue
			}
			flush()
			cur = []string{l}
			continue
		}
		out = append(out, l)
		for _, b := range boundaries {
			if l == "--"+b && i+1 < len(lines) {
				inHeader = true // a part's own headers follow
			}
		}
	}
	flush()
	res := strings.Join(out, "\r\n")
	if !strings.HasSuffix(res, "\r\n") {
		res += "\r\n"
	}
	return []byte(res)
}

// fold splits a header line at spaces so that no line exceeds max
// characters (continuation lines start with a space). Quoted strings are
// never split. A word longer than the limit stays whole (still < 998).
func fold(line string, max int) []string {
	if len(line) <= max {
		return []string{line}
	}
	var words []string
	start, inQ := 0, false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQ = !inQ
		case ' ':
			if !inQ && i > start {
				words = append(words, line[start:i])
				start = i + 1
			} else if !inQ {
				start = i + 1
			}
		}
	}
	words = append(words, line[start:])
	if len(words) > 1 && strings.HasSuffix(words[0], ":") {
		words = append([]string{words[0] + " " + words[1]}, words[2:]...) // keep "Name: value" together
	}
	var out []string
	cur := ""
	for _, w := range words {
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= max:
			cur += " " + w
		default:
			out = append(out, cur)
			cur = " " + w
		}
	}
	return append(out, cur)
}

// dispositionFor builds an attachment disposition with an ASCII fallback
// name and the exact UTF-8 name per RFC 2231 (split into short segments so
// the header folds cleanly), like Thunderbird does.
func dispositionFor(name string) string {
	ascii := true
	for i := 0; i < len(name); i++ {
		if name[i] >= 0x80 || name[i] < 0x20 || name[i] == '"' || name[i] == '\\' {
			ascii = false
			break
		}
	}
	if ascii && len(name) <= 60 {
		return `attachment; filename="` + name + `"`
	}
	var b bytes.Buffer
	b.WriteString("attachment")
	enc := strings.ReplaceAll(url.PathEscape(name), "+", "%2B")
	enc = strings.NewReplacer("'", "%27", "(", "%28", ")", "%29", "*", "%2A", ",", "%2C", ";", "%3B", ":", "%3A", "=", "%3D", "@", "%40", "&", "%26", "$", "%24").Replace(enc)
	// segments of at most ~50 characters, never cutting a %XX escape
	var segs []string
	for len(enc) > 50 {
		cut := 50
		if i := strings.LastIndexByte(enc[:cut], '%'); i > cut-3 {
			cut = i
		}
		segs = append(segs, enc[:cut])
		enc = enc[cut:]
	}
	segs = append(segs, enc)
	if len(segs) == 1 {
		b.WriteString("; filename*=utf-8''" + segs[0])
	} else {
		for i, s := range segs {
			if i == 0 {
				b.WriteString("; filename*0*=utf-8''" + s)
			} else {
				b.WriteString("; filename*" + itoa(i) + "*=" + s)
			}
		}
	}
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

// encodeHeaderText returns s unchanged when it is plain printable ASCII,
// otherwise as RFC 2047 "B" encoded words of at most ~50 characters each
// (split at character boundaries), so the folded header stays short.
func encodeHeaderText(s string) string {
	plain := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 || s[i] < 0x20 {
			plain = false
			break
		}
	}
	if plain && !strings.Contains(s, "=?") {
		return s
	}
	var words []string
	var chunk []byte
	for _, r := range s {
		if r < 0x20 {
			r = ' '
		}
		rb := []byte(string(r))
		if len(chunk)+len(rb) > 36 {
			words = append(words, mime.BEncoding.Encode("utf-8", string(chunk)))
			chunk = nil
		}
		chunk = append(chunk, rb...)
	}
	if len(chunk) > 0 {
		words = append(words, mime.BEncoding.Encode("utf-8", string(chunk)))
	}
	return strings.Join(words, " ")
}
