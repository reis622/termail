// Command termail is a terminal e-mail client for Windows (and other OSes).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/blocklist"
	"termail/internal/demo"
	"termail/internal/i18n"
	"termail/internal/install"
	"termail/internal/localstore"
	"termail/internal/single"
	"termail/internal/ui"
)

var version = "1.0.0"

// procStart is taken as early as Go allows (package initialisation), for
// the start-up timings.
var procStart = time.Now()

func main() {
	resolveLang()
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "kur", "install":
			if err := install.Install(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, i18n.T("Kurulum tamamlanamadı:"), err)
				os.Exit(1)
			}
			return
		case "kaldır", "kaldir", "uninstall":
			if err := install.Uninstall(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, i18n.T("Kaldırma tamamlanamadı:"), err)
				os.Exit(1)
			}
			return
		}
	}
	demoMode := flag.Bool("demo", false, i18n.T("örnek postalarla çevrimdışı deneme modu (hiçbir şey gönderilmez)"))
	demoNew := flag.Int("demo-newmail", 1, i18n.T("deneme modunda gelecek yeni ileti sayısı"))
	demoDelay := flag.Duration("demo-delay", 2*time.Second, i18n.T("deneme modunda yeni iletinin gelme süresi"))
	demoSpam := flag.Duration("demo-spam-after", 0, i18n.T("deneme modunda engelli bir alan adından ileti gelme süresi (0 = yok)"))
	login := flag.Bool("login", false, i18n.T("giriş ekranını göster"))
	timing := flag.Bool("zaman", false, i18n.T("açılış ve kapanış sürelerini günlük dosyasına yaz (TERMAIL_TIMING=1 ile de olur)"))
	ver := flag.Bool("version", false, i18n.T("sürümü göster"))
	flag.Usage = func() {
		o := flag.CommandLine.Output()
		fmt.Fprintln(o, i18n.T("Kullanım:"))
		fmt.Fprintln(o, i18n.T("  termail          programı açar"))
		fmt.Fprintln(o, i18n.T("  termail kur      programı bilgisayara kurar; sonra her terminalde termail yazman yeter"))
		fmt.Fprintln(o, i18n.T("  termail kaldır   kurulumu geri alır (ayarların ve girişlerin korunur)"))
		fmt.Fprintln(o, "\nSeçenekler:")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *ver {
		fmt.Println("Termail", version)
		return
	}
	opt := ui.Options{ShowLogin: *login, Start: procStart, Timing: *timing || os.Getenv("TERMAIL_TIMING") == "1"}
	if *demoMode {
		accts, mems := demo.Accounts(time.Now())
		opt.Accounts, opt.Demo = accts, true
		opt.Config = &account.Config{}
		opt.AfterStart = demo.NewMail(mems, *demoNew, *demoDelay, *demoSpam)
		opt.Blocks, _ = blocklist.Load("")
		account.UseMemoryKeyring()
	} else {
		if os.Getenv("TERMAIL_MEMKEYRING") == "1" { // developer aid for headless tests
			account.UseMemoryKeyring()
		}
		cfg, err := account.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("Ayarlar okunamadı:"), err)
			os.Exit(1)
		}
		opt.Config = cfg
		if pw := os.Getenv("TERMAIL_TEST_PASSWORD"); pw != "" && os.Getenv("TERMAIL_MEMKEYRING") == "1" {
			for _, a := range cfg.Accounts { // headless timing tests only; memory keyring, never logged
				_ = account.SetSecret(a.Email, account.SecretPassword, pw)
			}
		}
		dir, _ := account.Dir()
		opt.Blocks, err = blocklist.Load(filepath.Join(dir, "blocklist.json"))
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("Engellenenler listesi okunamadı:"), err)
		}
		if lf, err := openLog(dir); err == nil {
			defer lf.Close()
			opt.Logger = log.New(lf, "", log.LstdFlags)
			backend.DebugOut = lf
		}
		opt.Store = localstore.Open()
		// one real OS lock; a file left behind by a crash never warns
		if base, err := localstore.Base(); err == nil {
			lk, busy, _ := single.Acquire(base)
			opt.OtherInstance = busy
			opt.OnExit = lk.Release
			defer lk.Release()
		}
		// Nothing slow before the first frame: the saved sign-in (Windows
		// Credential Manager) is read in the background by Build, no server
		// is contacted and no settings are looked up (accounts in config.json
		// already carry their server settings).
		for _, a := range cfg.Accounts {
			a := a
			opt.Accounts = append(opt.Accounts, &ui.Acct{Cfg: a, Build: func() (backend.Backend, error) { return ui.BuildBackend(cfg, a) }})
		}
		if opt.Timing && opt.Logger != nil {
			opt.Logger.Printf("zaman %-40s %8.1f ms", "main: ayarlar okundu", float64(time.Since(procStart).Microseconds())/1000)
		}
	}
	if err := ui.Run(opt); err != nil {
		if opt.OnExit != nil {
			opt.OnExit()
		}
		fmt.Fprintln(os.Stderr, i18n.T("Termail beklenmedik şekilde kapandı:"), err)
		os.Exit(1)
	}
}

// openLog opens a small diagnostic log (never contains passwords or tokens).
func openLog(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "termail.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 1<<20 {
		os.Rename(p, p+".old")
	}
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

// resolveLang uses config.json "language" when set, otherwise the OS UI language.
func resolveLang() {
	lang := i18n.Detect()
	if cfg, err := account.Load(); err == nil && cfg != nil && cfg.Language != "" {
		if l, ok := i18n.Parse(cfg.Language); ok {
			lang = l
		}
	}
	i18n.Set(lang)
}
