// Package htmltext converts HTML mail bodies into readable plain text.
// External resources (images, scripts, styles) are never loaded; images are
// replaced by their alt text and links are shown inline.
package htmltext

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "header": true,
	"footer": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "ul": true, "ol": true, "table": true, "tr": true, "blockquote": true,
	"pre": true, "hr": true, "dl": true, "dt": true, "dd": true, "center": true,
	"form": true, "address": true, "main": true, "nav": true, "aside": true, "figure": true,
}

var skipTags = map[string]bool{
	"script": true, "style": true, "head": true, "title": true, "noscript": true,
	"template": true, "svg": true, "object": true, "iframe": true, "button": true,
}

type conv struct {
	b         strings.Builder
	pendingNL int  // newlines requested before next text
	spacePend bool // a collapsed space is pending
	atLineBeg bool
	pre       int
	quote     int
	listStack []int // 0 = ul, >0 = next ol number
	started   bool
	links     *Links // when set, links become numbered markers
	inAnchor  int
}

// Convert turns an HTML document or fragment into plain text.
func Convert(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src
	}
	c := &conv{atLineBeg: true}
	c.walk(doc)
	return CleanText(tidy(c.b.String()))
}

// ConvertLinks is Convert for the reader: each link keeps its text and gets
// a small numbered marker after it (see Marker) instead of the address in
// brackets; written addresses in the text are numbered too. The links are
// collected in set (shared numbering, duplicates get the same number).
func ConvertLinks(src string, set *Links) string {
	src = strings.ReplaceAll(src, string(MarkRune), "")
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return Linkify(src, set)
	}
	c := &conv{atLineBeg: true, links: set}
	c.walk(doc)
	return CleanText(tidy(c.b.String()))
}

// imgAlt is the alt text of the first image inside n.
func imgAlt(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "img" {
		return strings.TrimSpace(attr(n, "alt"))
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if a := imgAlt(ch); a != "" {
			return a
		}
	}
	return ""
}

func (c *conv) newline(n int) {
	if !c.started {
		return
	}
	if n > c.pendingNL {
		c.pendingNL = n
	}
	c.spacePend = false
}

func (c *conv) prefix() string {
	if c.quote == 0 {
		return ""
	}
	return strings.Repeat("> ", c.quote)
}

func (c *conv) flushNL() {
	if c.pendingNL > 0 {
		for i := 0; i < c.pendingNL; i++ {
			c.b.WriteString("\n")
			if i < c.pendingNL-1 && c.quote > 0 {
				c.b.WriteString(strings.TrimRight(c.prefix(), " "))
			}
		}
		c.pendingNL = 0
		c.atLineBeg = true
	}
}

func (c *conv) text(s string) {
	if c.links != nil && c.inAnchor == 0 {
		s = Linkify(s, c.links) // written addresses, numbered in reading order
	}
	if c.pre > 0 {
		for i, line := range strings.Split(s, "\n") {
			if i > 0 {
				c.b.WriteString("\n")
				c.atLineBeg = true
			}
			if line != "" {
				c.writeRaw(line)
			}
		}
		return
	}
	var out strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || r == '\u00a0' {
			if r == '\u00a0' {
				out.WriteRune(' ')
				continue
			}
			c.flushWord(&out)
			c.spacePend = true
			continue
		}
		if c.spacePend && !c.atLineBeg && c.pendingNL == 0 && out.Len() == 0 {
			c.writeRaw(" ")
		}
		c.spacePend = false
		out.WriteRune(r)
	}
	c.flushWord(&out)
}

func (c *conv) flushWord(out *strings.Builder) {
	if out.Len() == 0 {
		return
	}
	c.writeRaw(out.String())
	out.Reset()
}

func (c *conv) writeRaw(s string) {
	c.flushNL()
	if c.atLineBeg {
		c.b.WriteString(c.prefix())
	}
	c.b.WriteString(s)
	c.atLineBeg = false
	c.started = true
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			f(ch)
		}
	}
	f(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

func (c *conv) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		c.text(n.Data)
		return
	case html.CommentNode, html.DoctypeNode:
		return
	case html.ElementNode:
		tag := n.Data
		if skipTags[tag] {
			return
		}
		if hiddenStyle(attr(n, "style")) || hasAttr(n, "hidden") || strings.EqualFold(attr(n, "aria-hidden"), "true") && nodeText(n) == "" {
			return // hidden preheader / tracking padding
		}
		switch tag {
		case "br":
			c.started = true
			c.flushNL()
			c.b.WriteString("\n")
			c.atLineBeg = true
			c.spacePend = false
			return
		case "img":
			alt := strings.TrimSpace(attr(n, "alt"))
			if alt != "" {
				c.text(" [" + alt + "] ")
			}
			return
		case "hr":
			c.newline(1)
			c.writeRaw("────────────────────")
			c.newline(1)
			return
		case "a":
			href := strings.TrimSpace(attr(n, "href"))
			label := nodeText(n)
			if c.links != nil {
				if label == "" {
					label = imgAlt(n)
				}
				if label == "" { // empty or a bare (tracking) image: nothing to offer
					c.children(n)
					return
				}
				num := c.links.Add(href, label)
				c.inAnchor++
				c.children(n)
				c.inAnchor--
				if num > 0 {
					c.writeRaw(Marker(num))
				}
				return
			}
			c.children(n)
			if href != "" && !strings.HasPrefix(strings.ToLower(href), "javascript:") && !strings.HasPrefix(href, "#") {
				clean := strings.TrimPrefix(href, "mailto:")
				if clean != label && href != label {
					c.text(" (" + clean + ")")
				}
			}
			return
		case "li":
			c.newline(1)
			marker := "• "
			if l := len(c.listStack); l > 0 && c.listStack[l-1] > 0 {
				marker = itoa(c.listStack[l-1]) + ". "
				c.listStack[l-1]++
			}
			indent := strings.Repeat("  ", max(0, len(c.listStack)-1))
			c.writeRaw(indent + marker)
			c.spacePend = false
			c.children(n)
			c.newline(1)
			return
		case "ul", "ol":
			start := 0
			if tag == "ol" {
				start = 1
			}
			c.listStack = append(c.listStack, start)
			c.newline(1)
			c.children(n)
			c.listStack = c.listStack[:len(c.listStack)-1]
			c.newline(1)
			return
		case "td", "th":
			if c.started && !c.atLineBeg && c.pendingNL == 0 {
				c.writeRaw("  ")
			}
			c.spacePend = false
			c.children(n)
			c.spacePend = true
			return
		case "pre":
			c.newline(2)
			c.pre++
			c.children(n)
			c.pre--
			c.newline(2)
			return
		case "blockquote":
			c.newline(1)
			c.quote++
			c.children(n)
			c.newline(1)
			c.quote--
			return
		}
		if blockTags[tag] {
			nl := 1
			if tag == "p" || strings.HasPrefix(tag, "h") && len(tag) == 2 {
				nl = 2
			}
			c.newline(nl)
			c.children(n)
			c.newline(nl)
			return
		}
	}
	c.children(n)
}

func (c *conv) children(n *html.Node) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.walk(ch)
	}
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

// tidy trims trailing spaces and collapses 3+ blank lines into one blank line.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(strings.Trim(l, ">")) == "" && strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}
