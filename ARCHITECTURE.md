# Posta — Windows için terminal e-posta istemcisi (mimari taslağı)

> Durum: tasarım aşaması. Henüz kod yok. Uygulama adı ("Posta") geçici.
> Önizlemeler: `previews/01…05*.png` (kaynak: `previews/src/*.py`, karakter ızgarasıyla üretildi).

## 1. Hedefler
- Windows 10/11'de **WSL olmadan**, Windows Terminal / PowerShell içinde çalışan **tek `.exe`** (`posta.exe`, kurulum gerektirmez).
- **Hızlı ve sade:** açılışta yerel önbellekten anında liste gelir, ağ arka planda çalışır; hiçbir tuş ağı beklemez.
- Birden çok hesap (Gmail, Outlook/Hotmail, Yahoo, Yandex, iCloud, genel IMAP/SMTP).
- Kullanıcıya teknik terim gösterilmez; tüm metinler sade Türkçe. Koyu antrasit tema (saf siyah yok).

## 2. Kütüphane seçimleri
| İhtiyaç | Seçim | Gerekçe |
|---|---|---|
| Dil | Go 1.23+ | Tek statik `.exe`, CGO'suz çapraz derleme (`GOOS=windows`), hızlı açılış |
| TUI | `charmbracelet/bubbletea` + `lipgloss` + `bubbles` (list, textinput, textarea, viewport) | Windows Terminal'de sağlam, Elm tarzı mimari, test edilebilir |
| IMAP | `emersion/go-imap/v2` (imapclient) | IDLE, UIDPLUS, MOVE, CONDSTORE, SASL desteği; aktif bakım |
| SMTP | `emersion/go-smtp` + `emersion/go-sasl` | XOAUTH2/OAUTHBEARER özel SASL ile eklenebilir (`net/smtp` OAuth'ta yetersiz) |
| MIME ayrıştırma/oluşturma | `emersion/go-message` (mail paketi) | Türkçe karakter kümeleri, ekler, `multipart/alternative` |
| HTML → metin | `jaytaylor/html2text` (veya `k3a/html2text`) | HTML postayı terminalde okunur metne çevirir |
| Şifre / token saklama | `zalando/go-keyring` → Windows Credential Manager (wincred) | Düz metin config yok; kullanıcı hesabına bağlı DPAPI koruması |
| OAuth2 | `golang.org/x/oauth2` | Microsoft, PKCE, otomatik token yenileme |
| Yerel önbellek | `modernc.org/sqlite` (CGO'suz) + FTS5 | Anında açılış, çevrimdışı okuma, hızlı yerel arama |
| Config | TOML (`pelletier/go-toml/v2`) `%APPDATA%\Posta\config.toml` | Sadece hesap adı, adres, sunucu bilgisi — sır yok |
| Windows bildirimi (ops.) | `go-toast/toast` (PowerShell'siz WinRT çağrısı tercih) | Uygulama arka plandayken bildirim merkezi |
| DNS SRV | stdlib `net.LookupSRV` | Otomatik sunucu bulma |

## 3. Paket yapısı
```
cmd/posta/            main: bayraklar, config yolu, tea.Program başlatma
internal/ui/          Bubble Tea modelleri (görünüm katmanı, ağ yok)
  app.go              kök model: sekme çubuğu, odak, toast kuyruğu, global kısayollar
  tabs/               sekme yöneticisi (liste sekmesi sabit #1, ileti sekmeleri, yazma sekmeleri)
  sidebar/            hesaplar + klasörler paneli (hesaba göre gruplu, aç/kapat)
  maillist/           ileti listesi (sanal kaydırma, okunmamış/ek işaretleri)
  reader/             okuma görünümü (viewport, alıntı soluklaştırma, ek listesi)
  compose/            yazma/yanıtla/ilet formu, gönderen seçici
  login/              tek ekranlı giriş (adres + şifre / Microsoft tarayıcı girişi)
  help/               "?" kısayol ekranı
  toast/              uygulama içi bildirimler (yeni ileti, silindi/geri al, hata)
  theme/              lipgloss renk paleti ve stiller (tek yerde)
  keys/               tuş haritası (bubbles/key) — alt çubuk ve yardım buradan üretilir
internal/account/     hesap modeli, config okuma/yazma, keyring erişimi
internal/auth/        şifre, XOAUTH2 (Microsoft), token yenileme
internal/discover/    sunucu otomatik bulma (bilinen tablo → ISPDB/autoconfig → SRV → tahmin)
internal/imapx/       IMAP oturum havuzu, IDLE izleyici, senkron motoru
internal/smtpx/       gönderim, "Gönderilenler"e APPEND
internal/store/       SQLite önbellek (zarflar, gövdeler, bayraklar, FTS arama)
internal/mime/        ayrıştırma, HTML→metin, alıntı/imza, ileti oluşturma
internal/notify/      Windows bildirim merkezi (isteğe bağlı)
```

## 4. Veri akışı
```
           ┌────────────── tea.Msg ───────────────┐
 IMAP IDLE ─► imapx.Sync ─► store (SQLite) ─► ui (liste yenilenir, toast)
 kullanıcı tuşu ─► ui ─► tea.Cmd ─► imapx/smtpx (goroutine) ─► sonuç Msg ─► ui
```
- **UI asla ağ beklemez.** Tüm IMAP/SMTP işleri `tea.Cmd` içinde goroutine'de; sonuç `tea.Msg` olarak döner.
- **Hesap başına 3 IMAP bağlantısı:** (1) Gelen Kutusu'nda sürekli **IDLE** (yeni ileti anında itilir; klasör değiştirmek bunu etkilemez), (2) okuma (liste, gövde, arama), (3) değişiklikler (taşı, bayrak, boşalt) — iyimser işlem kuyruğu (`internal/opqueue`) bu bağlantıyı kullanır, böylece silme hiçbir zaman bir okumanın veya IDLE'ın arkasında beklemez. Kuyruk: posta kutusu başına sıralı; 300 ms içindeki uyumlu işlemler tek UID MOVE / UID STORE olur; hata olursa satır eski yerine döner. IDLE desteklemeyen sunucuda 60 sn'de bir kontrol; IDLE 25 dk'da bir yenilenir; bağlantı koparsa üstel geri çekilmeyle yeniden bağlanır.
- **Canlı klasör önizleme:** klasör panelinde ↑↓ → önbellekte varsa anında, yoksa 150 ms bekleme sonrası tek istek; eski liste yerinde kalır; istek kimliği tutmayan (eski) yanıtlar sadece önbelleğe yazılır.
- **Shift+D (Çöp/Spam boşalt):** sayı sunucudan alınır, katı onay (yalnız 'e'), 250'lik gruplar halinde STORE `\Deleted` + UID EXPUNGE (UIDPLUS yoksa EXPUNGE), ilerleme bildirimi.
- **Senkron:** açılışta önbellekten anında liste → arka planda UIDVALIDITY/UIDNEXT (+CONDSTORE varsa MODSEQ) ile fark çekme. Önce son 500 zarf, gövdeler açılınca (veya boşta) indirilir.
- **İyimser (optimistic) işlemler:** sil/arşivle/okundu anında ekranda uygulanır, sunucuya sonra gider; hata olursa geri alınır ve toast gösterilir.

## 5. Giriş ve hesap yönetimi (sihirbaz yok)
**Tek ekran:** e-posta adresi + şifre → Enter → doğrudan gelen kutusu. Liste arka planda dolarken arayüz hemen açılır.

1. Adres yazılırken alan adı tanınır ve sağ tarafta "Gmail ✓ / Outlook ✓" gösterilir.
2. **Sunucu otomatik bulma sırası** (`internal/discover`):
   1. Gömülü bilinen sağlayıcı tablosu: Gmail/Google Workspace (MX ile), Outlook/Hotmail/Live/MSN, Yahoo, Yandex, iCloud, GMX, Zoho, Proton Bridge (yerel), Turkcell/Superonline vb.
   2. Mozilla ISPDB / `autoconfig.<alan>` / `<alan>/.well-known/autoconfig`
   3. DNS SRV (`_imaps._tcp`, `_submission._tcp`)
   4. MX kaydından sağlayıcı tahmini (ör. MX google.com → Gmail ayarları)
   5. `imap.<alan>:993` / `smtp.<alan>:465|587` deneme
   6. Hepsi başarısızsa **yalnızca o zaman** "Sunucu ayarları" alanları açılır.
3. **Kimlik doğrulama türleri:**
   - **Microsoft (outlook.com, hotmail.*, live.*, msn.com, M365):** kişisel hesaplar artık şifreyle IMAP/SMTP kabul etmiyor → şifre alanı gizlenir, yerine **"Tarayıcıda Microsoft ile giriş yap"** gösterilir. OAuth2 **public client + PKCE**, **loopback redirect** (`http://localhost:<rastgele port>`, 127.0.0.1 ve ::1 dinlenir); tarayıcı açılamazsa adres ekranda + Ctrl+L; AADSTS50011/7000218 gibi hatalarda ya da 'k' ile **device code** akışına geçilir (kod büyük, 'c' kopyalar). Kapsamlar: `IMAP.AccessAsUser.All`, `SMTP.Send`, `offline_access`, `openid email profile` (adres id_token'dan). Yenilenemeyen oturum (invalid_grant) → hesap 'giriş gerekli'. IMAP/SMTP'de SASL **XOAUTH2**.
   - **Gmail:** yalnızca **uygulama şifresi** (ipucu: "Gmail'de normal şifre yerine uygulama şifresi gir"; **Ctrl + O harfi** sayfayı tarayıcıda açar ve adres ekranda da gösterilir). Google ile tarayıcı girişi bilerek yok (gömülü Google istemci kimliği yok).
   - **Diğerleri:** normal şifre (Yahoo/iCloud/Yandex için uygulama şifresi ipucu).
4. **Saklama:** `config.toml` yalnızca hesap adı, adres, sunucu, auth türü tutar. Şifre ve OAuth **refresh token** Windows Credential Manager'da (`go-keyring`, servis adı `Posta:<adres>`). Access token sadece bellekte; süresi dolmadan `x/oauth2.TokenSource` ile otomatik yenilenir, yeni refresh token tekrar keyring'e yazılır. Sonraki açılışlarda giriş sorulmaz.
   - Not: Credential Manager bir kayıtta ~2,5 KB sınırına sahip; büyük token'lar gerekirse parçalara bölünerek saklanır.
5. **Ek hesap:** `+` tuşu aynı tek ekranı açar. Hesap silme: klasör panelinde hesap üzerinde `Delete` → onay → config + keyring kaydı silinir.

## 6. Arayüz ve gezinme
- **Üst sekme çubuğu (görev çubuğu gibi):** #1 her zaman ileti listesi. Enter ile açılan her ileti ve her yeni/yanıt/ileti yazımı ayrı sekme. Aktif sekme belirgin, diğerleri soluk; okunmamış `●`, taslak `◇` işareti. Sekme sayısı ekranı aşarsa kaydırılır.
- **Ana ekran:** solda Hesaplar & Klasörler, ortada liste, sağda okuma önizlemesi. Dar pencerede (<110 sütun) okuma paneli alta iner, <80 sütunda gizlenir.
- **Odak kuralları:** liste ↔ `←` klasör paneli (↑↓ gez, Enter klasöre gir ve listeye dön, `→` listeye dön, Boşluk hesabı aç/kapat). Açık ileti sekmesinde `Esc` listeye döner.
- **Toast:** sağ altta, yumuşak renkli, 4 sn; birden fazlası üst üste yığılır. Yeni ileti toast'ı (hesap adıyla), "Silindi — u: geri al", hata/çevrimdışı uyarıları. İsteğe bağlı: uygulama penceresi odakta değilse Windows bildirim merkezi.
- **Arama:** `/` → önce yerel FTS (anında), ardından sunucuda IMAP `SEARCH` (Gmail'de `X-GM-RAW`) ile tamamlanır.

### Kısayollar (çakışmasız)
| Bağlam | Tuşlar |
|---|---|
| Her yer | `n` yeni ileti · `?` yardım · `Alt+1..9` sekmeye git · `Ctrl+Tab` / `Ctrl+Shift+Tab` sekme · `Ctrl+W` sekmeyi kapat · `q` çıkış (yalnız listede) |
| Liste | `↑↓` gez · `Enter` aç · `←` klasörler · `Tab`/`Shift+Tab` sekme · `r` yanıtla · `a`/`R` tümüne yanıtla · `f` ilet · `d`/`Delete` çöpe (onaysız) · `u` silmeyi geri al · `o` okundu/okunmadı · `s` yıldız · `e` arşiv · `m` taşı · `/` ara |
| Açık ileti | `Esc` listeye dön · `r a f` · `d` çöpe at ve sekmeyi kapat · `Tab` sekme |
| Klasör paneli | `↑↓` gez · `Enter` klasöre gir · `→` listeye dön · `Boşluk` hesabı aç/kapat · `+` hesap ekle |
| Yazma | `Tab`/`Shift+Tab` **alanlar arası** (bu yüzden sekme geçişi burada `Ctrl+Tab`/`Alt+1..9`) · `Ctrl+F` veya Gönderen alanında `← →` gönderen hesabı · `Ctrl+E` ek ekle · `Ctrl+Enter` gönder · `Esc` taslağa kaydet ve kapat |

Not: Bazı terminaller `Ctrl+Enter`'ı ayırt edemez; Windows Terminal'de çalışır, desteklenmezse `Ctrl+S` yedek gönder tuşu olur. Alt çubuk ve yardım ekranı tek kaynaktan (`internal/ui/keys`) üretilir.

## 7. Silme / geri alma
`d` → ileti anında listeden kalkar, arka planda `UID MOVE` ile Çöp'e (Gmail'de `[Gmail]/Çöp Kutusu`, klasör adları SPECIAL-USE ile bulunur). Son 10 işlem geri alma yığınında; `u` son işlemi Çöp'ten eski klasöre geri taşır (UIDPLUS ile yeni UID bilinir).

## 8. Gönderim
`go-message` ile MIME oluşturma → `go-smtp` (465 TLS / 587 STARTTLS, PLAIN/LOGIN/XOAUTH2) → Gmail dışı sağlayıcılarda "Gönderilenler"e IMAP `APPEND`. Gönderim kuyruğu: çevrimdışıysa "Giden" kuyruğunda bekler. Taslaklar 5 sn'de bir yerel kaydedilir, kapanışta sunucu "Taslaklar"ına yazılır.

## 9. Güvenlik
- Sırlar sadece Credential Manager'da; log'lara şifre/token yazılmaz.
- TLS zorunlu (sertifika hatasında bağlanmaz, kullanıcıya sade uyarı).
- HTML iletilerde dış görseller/izleme pikselleri yüklenmez (metne çevrilir); bağlantılar ekranda açıkça gösterilir.
- Önbellek `%LOCALAPPDATA%\Posta\cache.db`.

## 10. Derleme ve dağıtım
`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w"` (konsol uygulaması) → `posta.exe` (~15 MB). İsteğe bağlı: Scoop manifesti, winget paketi, ilk açılışta Windows Terminal profili ekleme.

## 11. İlk sürüm kapsamı (öneri)
Var: çoklu hesap, tek ekran giriş, otomatik sunucu bulma, Microsoft OAuth, Gmail uygulama şifresi, liste/klasörler/okuma/sekmeler, yaz/yanıtla/tümüne/ilet, sil/geri al, IDLE ile anlık yeni ileti + toast, yerel+sunucu arama.
Sonraya: konu (thread) görünümü, imzalar, kurallar/filtreler, PGP, takvim davetleri, HTML'i tarayıcıda açma (karar bekliyor).


## Sonradan eklenenler (uygulama notları)

- `internal/bodycache`: ileti gövdeleri için küçük LRU (60). Açık sekme, yazma penceresi ya da okuma alanının kullandığı gövdeler atılmaz.
- Okuma alanı imleci izler: önbellekteki gövde hemen görünür, yoksa imleç 120 ms durunca arka planda `BODY.PEEK` ile alınır ve komşular önceden yüklenir. İmlecin geçtiği iletiler için gelen yanıtlar yalnızca önbelleğe yazılır. Okundu işareti: Enter, `o` (tarayıcıda aç) ya da 3 sn seçili kalma (seçim sıra numarasıyla sıfırlanan zamanlayıcı).
- `focusReader`: listede Enter → okuma alanı; orada Enter → yanıtla (yeni sekme); Esc → liste.
- Çoklu hesap (`ui/accounts.go`): `Model.virt` = klasör / Tüm Gelen Kutuları / Kaydedilenler. Birleşik satırlar `Envelope.Acct/Folder` taşır, `m.loc(e)` çözer. Tuşlar: yalnızca F1–F12 ve Ctrl+A (F1 = Tüm Gelen Kutuları, F2 = 1. hesap). Rakam tuşları hesap değiştirmez. Yanıtta gönderen, iletinin geldiği hesaptır.
- Sonraya kaydet (`ui/saved.go`): `\Flagged`; liste `Backend.Flagged` ile (varsa yıldızlı/tüm posta özel klasörü, yoksa gelen/arşiv/gönderilen/kullanıcı klasörleri), Message-ID ile tekilleştirilir; anında uygulanır, sunucu reddederse geri alınır.
- Sunucu bulma: tahmini sunucuların gerçek bir posta sunucusu selamı vermesi gerekir (`* OK` / `220`, 993/465'te TLS); yalnızca açık port yetmez.
- Microsoft uygulama kimliği tek yerde: `internal/auth.DefaultMSClientID` (ya da `-ldflags "-X termail/internal/auth.DefaultMSClientID=…"`). Varsayılan: `456b7094-6cbb-4a4b-8b76-6a56fb79768e`.
- Ekran güvenliği (Windows'taki "kopyalanan satırlar" hatası): `grid.Clean` çizimden önce kontrol/görünmez karakterleri, emoji ve değişken genişlikli simgeleri ayıklar, NFC uygular; `Render` yarım kalmış geniş karakterleri onarır (her satır tam W hücre). `ui.finish` kareyi pencereye kırpar (`clampFrame`: tam pencere yüksekliği, en fazla genişlik−1, sonda satır sonu yok). Son sütun hiç yazılmaz (`edgeMargin`), böylece hemen satır kaydıran konsollarda bile ekran kaymaz. Kare her zaman tam yükseklik/genişlikte olduğu için Bubble Tea'nin satır farkı (diff) güvenle çalışır: yalnızca değişen satırlar yazılır (Windows'ta tüm ekranı yazmak yavaştır); kare hızı 60/sn ile sınırlı. Değişmeyen ekran birebir aynı kareyi üretir (test). Windows'ta boyut olay değerine değil konsol penceresine (`GetConsoleScreenBufferInfo.srWindow`) bakılarak alınır ve her saniye kontrol edilir; boyut bilinmeden kare 80×24'ü aşmaz. Çalışırken terminale hiçbir şey yazılmaz (`log` dosyaya/hiçliğe, protokol izi yalnızca log dosyasına).
- İleti çözme: yalnız `text/plain` ve `text/html` gövde sayılır; `text/rfc822-headers`, `message/delivery-status` ek olur; `message/rfc822` içindeki ileti çözülüp "İletilen ileti" olarak eklenir; bilinmeyen karakter kümesi tüm iletiyi düşürmez. Metin hâlâ ham başlıkla başlıyorsa (bozuk göndericiler) ayrıştırılır ya da atılır. Çözülemeyen ileti "Bu ileti görüntülenemedi." der, ham kaynak asla gösterilmez.
- `internal/install`: `termail kur` / `termail kaldır` (HKCU\Environment\Path, değer türü korunur, WM_SETTINGCHANGE yayını). PATH birleştirme saf fonksiyon, testli.

## Hız (tuş başına iş)

- Tuşa basınca ağ işi yapılmaz: gövde 150 ms durunca arka planda getirilir; çözümleme/HTML→metin arka planda.
- Çizim: yalnızca görünen satırlar; satır metinleri (temizlenmiş + kısaltılmış) ileti başına önbellekte; okuma panelinin satır kaydırması (wrap) ileti+genişlik başına önbellekte; renk kaçış dizileri stil başına bir kez hesaplanır (256/16 renkli konsolda renk dönüşümü çok pahalıydı); kare tamponu yeniden kullanılır; `visible()` çizim sırasında bir kez hesaplanır.
- Ölçüm: `go test ./internal/ui -bench Keypress` (2000 ileti, 160×40, Update+View). Önce ~2,5 ms / 12 381 ayırma, sonra ~0,22 ms / ~1 100 ayırma (bu makinede). `TestKeypressBudget` 5 ms sınırını korur.
- Gönderme: Ctrl+S sekmeyi hemen kapatır, gönderim arka planda; giden bağlantı yazma sekmesi açılınca hazırlanır ve 2 dk yeniden kullanılır. Gmail / Outlook'ta Gönderilenler'e kopya eklenmez (sunucu kendisi kaydeder).
- Giden ileti biçimi: CRLF, tüm başlık satırları ≤76 karakter (katlanır), Konu ve adlar RFC 2047 (B, kısa sözcükler), gövde quoted-printable, ek adları RFC 2231, base64 76'da sarılır, Bcc/X- başlığı yok; sıkı RFC denetimi testte.

## Bağlantılar ve fare

- `htmltext.ConvertLinks` / `htmltext.Linkify`: okuma metni (`Parsed.View`) bağlantı yazısının hemen ardına özel bir işaret karakteri (U+E000) + `[n]` koyar; bağlantılar `Parsed.Links` içinde numara sırasıyla durur. Yanıt/iletme alıntıları eskisi gibi `Parsed.Text` kullanır. Yalnız http/https/mailto kabul edilir (`htmltext.Normalize`); alan adı IDNA ile ASCII'ye çevrilir; boşluk, tırnak, denetim karakteri içeren adres reddedilir.
- Tarayıcı açma (`ui.browserCommands`): Windows'ta önce `rundll32 url.dll,FileProtocolHandler <adres>` (tek argüman), olmazsa `cmd /c start "" <adres>` ve adresteki `^&|<>()%!"` karakterleri `^` ile kaçırılır.
- Fare: `tea.WithMouseCellMotion()` (config `"fare": "kapalı"` ile kapanır). Her kare çizilirken tıklanabilir yerler bir isabet haritasına (`hitMap`) yazılır; tık en üstteki hedefe gider, açık bir pencere (yardım, onay, seçiciler) altını kapatır. Düğmesiz hareket/bırakma olayları yeni kare üretmez (önceki kare aynen döner).

## Açılış ve kapanış

- `main` tea.Program'dan önce yalnızca config.json'ı, engel listesini ve günlük dosyasını açar. Kimlik Bilgileri Yöneticisi okuması (`ui.BuildBackend`) her hesap için `Acct.Build` içinde, ilk kareden sonra arka planda çalışır; ayarlı hesaplar için sağlayıcı arama (autodiscover/DNS) yapılmaz.
- Init: yerel veriler (`internal/localstore`: gelen kutusu başlık önbelleği, bekleyen işlemler, yarım iletiler) arka planda okunur; sunucu listesi henüz gelmediyse önbellekteki liste gösterilir. Her hesap ayrı goroutine'de: giriş bilgisi → bağlan + giriş → IDLE (Watch), gelen kutusu ilk 60 ileti, sonra tam liste ve klasörler. SMTP yalnızca yazarken ısıtılır.
- Önbellek: `cache/gelen-<sha256(e-posta)[:8]>.json`, sürüm numaralı JSON, atomik yazım (geçici dosya + rename), yalnız başlıklar. Bozuk, kesik, başka sürüm, başka hesap ya da 16 MB'tan büyük dosya sessizce yok sayılır.
- Bağlanmadan yapılan işlemler `Model.replay`'de bekler; bağlantı kurulunca opqueue'ya verilir.
- Çıkış (`exitNow`): bekleyen işlemler `bekleyen-islemler.json`'a, kirli yazma sekmeleri ve gönderilmekte olanlar `taslaklar/`a yazılır; ardından `Program.Kill` (son çizim ve Windows'taki 500 ms okuma döngüsü beklemesi atlanır, terminal yine geri yüklenir). `IMAP.Close` LOGOUT/DONE/QUIT göndermez, soketleri paralel kapatır ve en çok 150 ms bekler; toplam bütçe 200 ms.
- Pencere kapatma: Windows'ta `SetConsoleCtrlHandler` (CTRL_CLOSE/LOGOFF/SHUTDOWN), diğerlerinde SIGHUP. İşleyici modele `closeMsg` gönderir (en çok 250 ms), kaydeder, bağlantıları 100 ms bütçeyle kapatır ve çıkar.
- `--zaman` / `TERMAIL_TIMING=1`: süreler günlüğe `zaman …` satırları olarak yazılır.

## Kopyalama, yapıştırma ve seçim

- `internal/edit`: bubbles'ın textinput/textarea'sının yerini alan kendi metin düzenleyicimiz (tek satır, adres, çok satır). Satırlar `[]rune`; imleç + bağlantı noktası (anchor) ile seçim; Shift/Ctrl+Shift oklar, Home/End; seçimi değiştiren yazma/silme; 100 adımlık geri al / yinele (art arda yazılan harfler tek adım). Çizim ızgaraya doğrudan yapılır, genişlik `grid.Clean` + `runewidth` ile hesaplanır (Türkçe/CJK güvenli), metin w-1 hücrede kelime kaydırılır. `Key` bir `Result{Copy, Paste, Cut}` döndürür; pano işleri arayüzde yapılır, böylece paket test edilebilir ve panoya dokunmaz. Şifre alanı (`Mask`) hiçbir zaman kopya vermez.
- `edit.Normalize`: ANSI kaçışları, kontrol karakterleri, BOM ve yön değiştirme karakterleri atılır; CRLF/CR → LF, sekme → 4 boşluk, NFC. Tek satırda satırlar boşlukla, adres alanında `,` ile birleşir. Üst sınır 1 MiB metin / 8000 karakterlik satır.
- `ui/clipboard.go`: Windows'ta `atotto/clipboard` (CGO yok), diğerlerinde OSC52 + aynı kütüphane. Ctrl+V ile köşeli parantezli yapıştırma (bracketed paste) aynı içerikle 700 ms içinde gelirse ikincisi atılır (Windows Terminal Ctrl+V'yi kendisi yapıştırır). Yazma sekmesinde bildirimler metni örtmesin diye notlar alttaki durum satırına yazılır.
- `ui/textsel.go`: okuma bölmesinde seçim, kaydırılmış satırlar üzerinde (görünür satır, hücre sütunu) olarak tutulur; `wrapSpans` her görünür satırın paragraftaki bayt aralığını verir, kopyalarken yumuşak satır kırılmaları arasındaki boşluk özgün metinden alınır (araya satır sonu eklenmez), paragraflar arasına `\n` konur, bağlantı numaraları çıkarılır. Kenarda sürüklerken 60 ms'lik bir tik metni kaydırır. Tuşsuz fare hareketi hâlâ çizim yaptırmaz.

## Arama, gruplama, hesap ağacı

- `internal/search`: sorgu ayrıştırma (`kimden:`, `ekli`, `okunmamış`, `kaydedilen`, `bugün/dün/bu hafta/bu ay`, tırnaklı ifade), Türkçe katlama (`Fold`: her rune tek rune'a eşlenir, böylece vurgu konumları korunur; İ/ı/I/i ve aksanlar), `Match`, `Spans`, `Snippet`; `group.go`: küçük bir eTLD+1 tablosu (co.uk, com.tr, …) ile `Registrable`, ücretsiz posta alan adlarında (gmail.com vb.) adres bazlı grup anahtarı.
- Yerel arama her tuşta senkron çalışır: önbellekteki başlıklar + `bodycache.Peek` (sıra değiştirmez); katlanmış metinler önbelleklenir (400 ileti + gövde: ~1 ms/harf). 350 ms bekleme sonrası sunucu araması `backend.CriteriaSearcher` ile ayrı bir IMAP oturumunda (`srch`) çalışır; yeni harf eski bağlamı iptal eder, `seq` ile eski cevaplar yok sayılır. Gmail'de yalnızca All Mail aranır; sonuçlar hesap+Message-ID ile tekilleştirilir (X-GM-MSGID zarfta yok).
- Gruplama görünüm katmanıdır: `computeVisible` grup başlıkları için yer tutucu zarf (`UID 0`, `Folder "\x00grp:"+anahtar`) üretir; `selected()` bunları döndürmez, bu yüzden hiçbir ileti işlemi başlığa uygulanamaz.
- Tercihler `localstore.Prefs` → `cache/arayuz.json` (son 10 sorgu metni, gruplama, açık hesaplar, grup son açılma zamanları); arka planda atomik yazılır.
- Vurgu, çizimden sonra hücre üzerinde ikinci geçiştir (`hlCells`); fark tabanlı çizici, genişlik−1 sınırı ve 60 fps sınırı değişmedi.

## Klavye (AltGr) ve tek pencere kilidi

- Bubble Tea `src/third_party/bubbletea` altında yamalı kopya olarak kullanılır (`go.mod` replace). `altgr.go`: Windows konsolundan Ctrl+Alt / AltGr ile gelen yazdırılabilir karakterler (Türkçe Q'da `@`, `€`, `{` …; Windows Terminal ve conhost'un yapıştırmayı tuş olayı olarak göndermesi dahil) düz harf olarak iletilir. Ek olarak `ui.textKey`, Alt işaretli tek bir yazdırılabilir karakterden (Alt+harf/rakam hariç) Alt'ı kaldırır.
- `internal/single`: `%LOCALAPPDATA%\Termail\termail.lock` üzerinde LockFileEx (Windows) / flock. Yalnız gerçekten çalışan başka bir Termail varken uyarı verilir; kilidi işletim sistemi süreç bitince (çökmede de) bırakır.
