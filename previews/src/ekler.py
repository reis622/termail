# -*- coding: utf-8 -*-
"""Preview mockups: Mail / Ekler ve Bağlantılar tabs on the reading pane.
Does not touch app source or termail.exe.
"""
import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tui import *

W, H = 140, 40
LW, MW = 28, 56
RW = W - LW - MW  # 56
TOP = 2
PH = H - TOP - 1  # 37
MX = LW + 2       # 30
MWI = MW - 4      # 52
RX = LW + MW + 2  # 86
RWI = RW - 4      # 52
TIME = "19:45"

TYPE_STYLE = {
    "PDF":   (T["red"], T["bg"]),
    "Resim": (T["yellow"], T["bg"]),
    "Tablo": (T["green"], T["bg"]),
    "Metin": (T["accent"], T["bg"]),
    "Arşiv": (T["purple"], T["bg"]),
}

ATTS = [
    ("PDF",   "Toplantı notları.pdf", "84 KB"),
    ("Tablo", "Bütçe 2026.csv",       "12 KB"),
    ("Resim", "Ofis krokisi.png",     "240 KB"),
]

# label = descriptive text preceding the link in the mail
LINKS = [
    # n, label, domain, mismatch
    ("1", "Toplantıya katılmak için tıkla",            "meet.firma.com",     False),
    ("2", "Güncel dosyalar proje klasöründe",          "dosyalar.firma.com", False),
    ("3", "Şirket duyurusunu buradan oku",             "www.firma.com",      False),
    ("4", "Ödemeyi güvenle tamamlamak için tıkla",     "odeme-guvenlik.xyz", True),
    ("5", "Toplantıyı takvime ekle",                   "takvim.firma.com",   False),
]

BODY = [
    "Merhaba Ahmet,",
    "",
    "Yarınki toplantı için gereken dosyaları",
    "ekledim:",
    "",
    "  •  Toplantı notları (PDF)",
    "  •  Bu yılın bütçe tablosu",
    "  •  Ofis krokisi",
    "",
    "Toplantıya katılmak için tıkla [1].",
    "Güncel dosyalar proje klasöründe [2].",
    "Şirket duyurusunu buradan oku [3].",
    "",
    "Ödemeyi güvenle tamamlamak için tıkla [4].",
    "Toplantıyı takvime ekle [5].",
    "",
    "Görüşmek üzere,",
    "Ayşe Demir",
]


def header(s, time=TIME):
    s.fill(0, 0, W, 1, T["bar"])
    s.put(1, 0, " ◆ Termail ", T["bg"], T["accent"], bold=True)
    s.put(14, 0, "ahmet.yilmaz@gmail.com", T["dim"], T["bar"])
    pills = [(1, "Kişisel", 8), (2, "İş", 2), (3, "Okul", 0)]
    x = 38
    for num, name, unread in pills:
        active = num == 1
        bg = T["selfocus"] if active else T["bar"]
        fg = T["fg"] if active else T["dim"]
        s.put(x, 0, " ", fg, bg); x += 1
        s.put(x, 0, str(num), T["accent"] if active else T["faint"], bg, bold=active); x += 1
        s.put(x, 0, " " + name, fg, bg, bold=active); x += 1 + len(name)
        if unread:
            s.put(x, 0, " ", fg, bg); x += 1
            s.put(x, 0, str(unread), T["yellow"], bg, bold=True); x += len(str(unread))
        s.put(x, 0, " ", fg, bg); x += 1
        x += 1
    right = f"Çevrimiçi  ● {time} "
    s.rput(W - 1, 0, right, fg=T["dim"], bg=T["bar"])
    s.put(W - 1 - len(right) + right.index("●"), 0, "●", T["green"], T["bar"])


def tabbar(s):
    s.fill(0, 1, W, 1, T["bar"])
    tw = 22
    s.fill(1, 1, tw, 1, T["selfocus"])
    s.put(1, 1, "▌", T["accent"], T["selfocus"])
    s.put(3, 1, "1", T["accent"], T["selfocus"], bold=True)
    s.put(5, 1, "≡ Gelen Kutusu", T["fg"], T["selfocus"], bold=True)


def folders(s):
    s.box(0, TOP, LW, PH, fg=T["border"], bg=T["panel"], title="Klasörler")
    y = TOP + 2

    def row(y, label, count=None, active=False, icon=None, indent=0,
            count_yellow=False, faint=False):
        bg = None
        if active:
            s.fill(1, y, LW - 2, 1, T["sel"])
            bg = T["sel"]
            s.put(2, y, "▌", T["accent"], bg)
        elif icon:
            s.put(2, y, icon, T["fg"] if icon == "▾" else T["dim"])
        text_x = 4 + indent
        if faint:
            fg, bold = T["faint"], False
        else:
            fg = T["fg"] if active or icon == "▾" else T["dim"]
            bold = active or icon == "▾"
        room = (LW - 3) - text_x - (3 if count is not None else 0)
        s.put(text_x, y, label, fg, bg, bold=bold, maxw=max(room, 4))
        if count is not None and count != "":
            s.rput(LW - 2, y, str(count),
                   fg=T["yellow"] if (count_yellow or active) else T["faint"],
                   bg=bg, bold=active or count_yellow)
        return y + 1

    y = row(y, "Tüm Gelen Kutuları", 10, icon="≡", count_yellow=True)
    y = row(y, "Kaydedilenler", 5, icon=" ")
    y = row(y, "Kişisel", 8, icon="▾", count_yellow=True)
    y = row(y, "Gmail", faint=True)
    y = row(y, "Gelen Kutusu", 8, active=True, count_yellow=True)
    y = row(y, "Yıldızlı")
    y = row(y, "Gönderilenler")
    y = row(y, "Taslaklar", 1)
    y = row(y, "Arşiv")
    y = row(y, "Spam")
    y = row(y, "Çöp Kutusu")
    y = row(y, "Klasörlerim", faint=True)
    y = row(y, "Aile", indent=2)
    y = row(y, "Faturalar", indent=2)
    s.put(1, y, "─" * (LW - 2), T["border"]); y += 1
    y = row(y, "İş", 3, icon="▸", count_yellow=True)
    y = row(y, "Outlook", faint=True)
    y = row(y, "Okul", icon="▸")
    y = row(y, "Diğer", faint=True)
    s.put(1, TOP + PH - 4, "─" * (LW - 2), T["border"])
    s.put(2, TOP + PH - 3, "⊕ Engellenenler", T["dim"])
    s.rput(LW - 2, TOP + PH - 3, "Ctrl+B", fg=T["faint"])
    s.put(2, TOP + PH - 2, "+ Hesap ekle", T["accent"])
    s.rput(LW - 2, TOP + PH - 2, "+", fg=T["faint"])


def list_pane(s):
    s.box(LW, TOP, MW, PH, fg=T["border"], bg=T["bg"], title="Kişisel · Gelen Kutusu")
    s.fill(MX, TOP + 1, MWI, 1, T["raised"])
    s.put(MX + 1, TOP + 1, "Ara: ", T["faint"], T["raised"])
    s.put(MX + 6, TOP + 1, "gönderen, konu veya içerik…", T["faint"], T["raised"])
    s.rput(MX + MWI - 1, TOP + 1, "/", fg=T["dim"], bg=T["raised"])
    s.put(MX, TOP + 2, "25 ileti · 8 okunmamış", T["faint"])
    s.put(LW + 1, TOP + 3, "─" * (MW - 2), T["border"])
    rows = [
        # sel, unr, frm, sub, date, att
        (True,  True,  "Ayşe Demir",     "Proje toplantısı – dosyalar",   "18:40", True),
        (False, True,  "Mehmet Kaya",    "Akşam yemeği?",                 "14:31", False),
        (False, True,  "Google",         "Güvenlik uyarısı: yeni oturum", "13:14", False),
        (False, True,  "Turkcell",       "Kasım 2026 faturanız hazır",    "13:05", True),
        (False, True,  "GitHub",         "[notify-guard] v0.2 derleme",   "11:48", False),
        (False, True,  "Site Yönetimi",  "Aidat ödemeniz alındı",         "06:33", False),
        (False, False, "Trendyol",       "Siparişiniz kargoya verildi",   "Dün",   False),
        (False, True,  "Muhasebe",       "Ekim ayı ekstresi (PDF)",       "Dün",   True),
        (False, False, "Zeynep Arslan",  "Fotoğraflar",                   "12 Kas",True),
        (False, False, "Banka",          "Ekstre bildirimi",              "11 Kas",True),
        (False, False, "Spotify",        "Bu haftanın keşifleri",         "10 Kas",False),
        (False, False, "Elif Şahin",     "Re: Doğum günü sürprizi",       "9 Kas", False),
        (False, False, "Udemy",          "Kursunuza yeni bölüm eklendi", "8 Kas", False),
        (False, False, "Burak Yıldız",   "Sunum dosyası",                 "7 Kas", True),
        (False, False, "LinkedIn",       "Profilinizi 12 kişi görüntüledi","7 Kas", False),
        (False, False, "Selin Aydın",    "Teşekkürler!",                  "6 Kas", False),
        (False, False, "Hepsiburada",    "Favorinizde indirim var",       "5 Kas", False),
    ]
    y = TOP + 4
    for sel, unr, frm, sub, date, att in rows:
        if y >= TOP + PH - 1:
            break
        bg = T["sel"] if sel else None  # list not focused; sel not selfocus
        if bg:
            s.fill(LW + 1, y, MW - 2, 1, bg)
        s.put(MX, y, "●" if unr else " ", T["accent"], bg)
        s.put(MX + 2, y, frm, T["fg"] if unr else T["dim"], bg, bold=unr, maxw=14)
        s.put(MX + 17, y, sub, T["fg"] if unr else T["dim"], bg, bold=unr and sel, maxw=24)
        s.put(MX + 43, y, "▪" if att else " ", T["purple"], bg)
        s.rput(MX + MWI, y, date, fg=T["fg"] if unr else T["dim"], bg=bg)
        y += 1


def reader_box(s, title="Okuma"):
    s.box(LW + MW, TOP, RW, PH, fg=T["borderf"], bg=T["bg"], title=title)


def reader_tabs(s, active="mail", n_att=3, n_link=5):
    """Two clickable tabs just under the Okuma title. Returns first content row."""
    y = TOP + 1
    s.fill(LW + MW + 1, y, RW - 2, 1, T["panel"])
    t1 = " Mail "
    t2 = f" Ekler ve Bağlantılar ({n_att} · {n_link}) "
    x = RX
    if active == "mail":
        s.fill(x, y, len(t1), 1, T["selfocus"])
        s.put(x, y, "▌", T["accent"], T["selfocus"])
        s.put(x + 1, y, t1[1:], T["fg"], T["selfocus"], bold=True)
        s.put(x + len(t1) + 1, y, t2, T["dim"], T["panel"])
    else:
        s.put(x, y, t1, T["dim"], T["panel"])
        x2 = x + len(t1) + 1
        s.fill(x2, y, len(t2), 1, T["selfocus"])
        s.put(x2, y, "▌", T["accent"], T["selfocus"])
        s.put(x2 + 1, y, t2[1:], T["fg"], T["selfocus"], bold=True)
    s.put(LW + MW + 1, y + 1, "─" * (RW - 2), T["border"])
    return y + 2


def type_chip(s, x, y, typ):
    cbg, cfg = TYPE_STYLE[typ]
    label = f" {typ} "
    s.put(x, y, label, cfg, cbg, bold=True)
    return x + len(label)


def mail_content(s, y):
    hdr = [
        ("Kimden", "Ayşe Demir <ayse.demir@firma.com>"),
        ("Kime",   "ahmet.yilmaz@gmail.com"),
        ("Tarih",  "14 Kasım 2026, 18:40"),
        ("Konu",   "Proje toplantısı – dosyalar"),
        ("Ek",     "3 dosya"),
    ]
    for k, v in hdr:
        s.put(RX, y, f"{k:<7}", T["faint"])
        fg = T["purple"] if k == "Ek" else T["fg"]
        s.put(RX + 8, y, v, fg, bold=(k == "Konu"), maxw=RWI - 8)
        y += 1
    s.put(LW + MW + 1, y, "─" * (RW - 2), T["border"])
    y += 2
    for line in BODY:
        if y >= TOP + PH - 2:
            break
        s.put(RX, y, line, T["fg"] if line.strip() else None, maxw=RWI)
        y += 1
    s.put(RX, TOP + PH - 2, "Enter: yanıtla  ·  Esc: listeye dön  ·  e: ekler",
          T["faint"], maxw=RWI)


def att_row(s, y, typ, name, size, sel=False):
    bg = T["selfocus"] if sel else None
    if bg:
        s.fill(LW + MW + 1, y, RW - 2, 1, bg)
        s.put(LW + MW + 1, y, "▌", T["accent"], bg)
    x = type_chip(s, RX, y, typ)
    s.put(x + 1, y, name, T["fg"], bg, bold=sel, maxw=RWI - (x + 1 - RX) - 8)
    s.rput(RX + RWI, y, size, fg=T["dim"], bg=bg)
    return y + 1


def link_block(s, y, n, label, domain, mismatch, sel=False, y_stop=None):
    """Label (from preceding sentence) + domain beneath; warning if mismatch."""
    bg = T["selfocus"] if sel else None
    # two or three lines
    lines = 3 if mismatch else 2
    if y_stop is not None and y + lines > y_stop:
        return y
    if bg:
        for i in range(lines):
            s.fill(LW + MW + 1, y + i, RW - 2, 1, bg)
            if i == 0:
                s.put(LW + MW + 1, y, "▌", T["accent"], bg)
    mark = f"[{n}]"
    s.put(RX, y, mark, T["red"] if mismatch else T["accent"], bg, bold=True)
    s.put(RX + 5, y, label, T["red"] if mismatch else T["fg"], bg, maxw=RWI - 5)
    y += 1
    s.put(RX + 5, y, domain, T["red"] if mismatch else T["dim"], bg, maxw=RWI - 5)
    y += 1
    if mismatch:
        s.put(RX + 5, y, "Dikkat: farklı adrese gidiyor", T["red"], bg, bold=True,
              maxw=RWI - 5)
        y += 1
    return y


def csv_preview(s, y, y_stop):
    s.put(RX, y, "Önizleme  ·  Bütçe 2026.csv", T["accent"], bold=True)
    y += 1
    s.put(RX, y, f"{'Kalem':<12}{'Plan':>10}{'Gerçek':>10}", T["faint"])
    y += 1
    s.put(RX, y, "─" * min(RWI, 32), T["border"]); y += 1
    rows = [
        ("Yazılım", "12.000", "11.450"),
        ("Donanım",  "8.500",  "9.200"),
        ("Ofis",     "3.200",  "3.150"),
        ("Eğitim",   "2.000",  "1.800"),
    ]
    for name, plan, real in rows:
        if y >= y_stop:
            break
        s.put(RX, y, f"{name:<12}{plan:>10}{real:>10}", T["fg"])
        y += 1
    if y < y_stop:
        s.put(RX, y, "─" * min(RWI, 32), T["border"]); y += 1
    if y < y_stop:
        s.put(RX, y, f"{'Toplam':<12}{'25.700':>10}", T["fg"], bold=True)
        s.put(RX + 22, y, f"{'25.600':>10}", T["yellow"], bold=True)
        y += 1
    return y


# ---------------------------------------------------------------------------
# 1. Mail tab active; second tab shows counts
# ---------------------------------------------------------------------------
def shot1():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s); list_pane(s)
    reader_box(s, title="Okuma")
    y = reader_tabs(s, active="mail")
    mail_content(s, y)
    s.keybar(H - 1, [
        ("Esc", "Listeye dön"),
        ("↑↓", "Kaydır"),
        ("Enter", "Yanıtla"),
        ("e", "Ekler ve bağlantılar"),
        ("k", "Kaydet"),
        ("d", "Sil"),
    ], right="? Yardım")
    return s


# ---------------------------------------------------------------------------
# 2. Second tab: attachments + labelled links, one warned
# ---------------------------------------------------------------------------
def shot2():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s); list_pane(s)
    reader_box(s, title="Okuma")
    y = reader_tabs(s, active="ekler")
    s.put(RX, y, "Ekler", T["accent"], bold=True); y += 1
    for typ, name, size in ATTS:
        y = att_row(s, y, typ, name, size, sel=False)
    y += 1
    s.put(RX, y, "Bağlantılar", T["accent"], bold=True); y += 1
    stop = TOP + PH - 2
    for n, label, domain, mismatch in LINKS:
        y = link_block(s, y, n, label, domain, mismatch, y_stop=stop)
        if y >= stop:
            break
    s.put(RX, TOP + PH - 2, "Enter: aç  ·  s: İndirilenler'e kaydet  ·  e: Mail",
          T["faint"], maxw=RWI)
    s.keybar(H - 1, [
        ("↑↓", "Gezin"),
        ("Enter", "Aç"),
        ("s", "İndirilenler'e kaydet"),
        ("e", "Mail"),
        ("Esc", "Mail"),
    ], right="? Yardım")
    return s


# ---------------------------------------------------------------------------
# 3. CSV attachment selected with table preview
# ---------------------------------------------------------------------------
def shot3():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s); list_pane(s)
    reader_box(s, title="Okuma")
    y = reader_tabs(s, active="ekler")
    s.put(RX, y, "Ekler", T["accent"], bold=True); y += 1
    for i, (typ, name, size) in enumerate(ATTS):
        y = att_row(s, y, typ, name, size, sel=(i == 1))
    y += 1
    # preview immediately under the attachments (selected item)
    s.put(LW + MW + 1, y, "─" * (RW - 2), T["border"]); y += 1
    y = csv_preview(s, y, y_stop=TOP + PH - 12)
    y += 1
    s.put(RX, y, "Bağlantılar", T["accent"], bold=True); y += 1
    stop = TOP + PH - 2
    for n, label, domain, mismatch in LINKS:
        y = link_block(s, y, n, label, domain, mismatch, y_stop=stop)
        if y >= stop:
            break
    s.put(RX, TOP + PH - 2, "Enter: aç  ·  s: İndirilenler'e kaydet  ·  e: Mail",
          T["faint"], maxw=RWI)
    s.keybar(H - 1, [
        ("↑↓", "Gezin"),
        ("Enter", "Aç"),
        ("s", "İndirilenler'e kaydet"),
        ("e", "Mail"),
        ("Esc", "Mail"),
    ], right="? Yardım")
    return s


if __name__ == "__main__":
    jobs = [
        (shot1, "ekler-1-mail-sekmesi", "Termail"),
        (shot2, "ekler-2-liste", "Termail"),
        (shot3, "ekler-3-onizleme", "Termail"),
    ]
    for fn, name, title in jobs:
        print(render(fn(), name, title))
