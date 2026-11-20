// Command localmail runs a local in-memory mail server pair for manual
// end-to-end testing of Termail (developer tool, not shipped to users).
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"

	"termail/internal/mailmsg"
	"termail/internal/samplemail"
	"termail/internal/testserver"
)

func main() {
	s, err := testserver.Start("127.0.0.1:1143", "127.0.0.1:1025")
	if err != nil {
		panic(err)
	}
	s.AddUser("ahmet@test.local", "gizli")
	deliver := func(name, addr, subj, body string, att bool) {
		o := &mailmsg.Outgoing{From: &mail.Address{Name: name, Address: addr}, To: []*mail.Address{{Address: "ahmet@test.local"}}, Subject: subj, Body: body}
		if att {
			o.Attachments = []mailmsg.Attachment{{Name: "rapor.pdf", Data: []byte("%PDF-1.4 yerel test")}}
		}
		raw, _ := mailmsg.Build(o)
		s.Deliver("ahmet@test.local", "INBOX", raw)
	}
	deliver("Mehmet Kaya", "mehmet@ornek.com", "Akşam yemeği?", "Bu akşam?", false)
	deliver("Muhasebe", "muhasebe@ornek.com", "Rapor ekte", "Ekteki raporu inceleyin.", true)
	deliver("Fırsat", "kampanya@reklam.com", "İndirim!", "Kaçırma", false)
	for _, sm := range samplemail.All(time.Now()) { // realistic provider mails
		s.Deliver("ahmet@test.local", "INBOX", sm.Raw)
	}
	fmt.Printf("IMAP %s  SMTP %s  kullanıcı ahmet@test.local / gizli\n", s.IMAPAddr, s.SMTPAddr)
	fmt.Println("stdin: 'new <konu>' yeni ileti, 'spam' reklam.com'dan ileti, 'sent' son gönderilen")
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "new"):
				deliver("Ayşe Demir", "ayse@ornek.com", strings.TrimSpace(strings.TrimPrefix(line, "new"))+" "+time.Now().Format("15:04:05"), "Yeni ileti", false)
				fmt.Println("delivered")
			case line == "spam":
				deliver("Fırsat", "yeni@mail.reklam.com", "Yine indirim", "spam", false)
				fmt.Println("delivered spam")
			case line == "sent":
				if r, ok := s.LastReceived(); ok {
					fmt.Printf("from=%s to=%v bytes=%d\n", r.From, r.To, len(r.Data))
				}
			}
		}
	}()
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}
