# -*- coding: utf-8 -*-
"""Preview mockups: search (3), sender grouping (2), account tree (2).
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
TIME = "15:03"

ACCT_FG = {"Kişisel": T["dim"], "İş": T["yellow"], "Okul": T["purple"]}


def put_hl(s, x, y, text, needles, fg=None, bg=None, bold=False, maxw=None,
           hlfg=None, hlbg=None):
    fg = fg or T["fg"]
    hlfg = hlfg or T["bg"]
    hlbg = hlbg or T["yellow"]
    if maxw is not None and len(text) > maxw:
        text = text[:maxw - 1] + "…"
    mark = [False] * len(text)
    low = text.casefold()
    for n in needles:
        nl = n.casefold()
        if not nl:
            continue
        start = 0
        while True:
            i = low.find(nl, start)
            if i < 0:
                break
            for k in range(i, i + len(nl)):
                mark[k] = True
            start = i + len(nl)
    i = 0
    while i < len(text):
        j = i + 1
        while j < len(text) and mark[j] == mark[i]:
            j += 1
        if mark[i]:
            s.put(x + i, y, text[i:j], hlfg, hlbg, bold=True)
        else:
            s.put(x + i, y, text[i:j], fg, bg, bold=bold)
        i = j
    return x + len(text)


def header(s, time=TIME, identity="ahmet.yilmaz@gmail.com",
           pills=None, active_pill=1):
    if pills is None:
        pills = [(1, "Kişisel", 8), (2, "İş", 2), (3, "Okul", 0)]
    s.fill(0, 0, W, 1, T["bar"])
    s.put(1, 0, " ◆ Termail ", T["bg"], T["accent"], bold=True)
    s.put(14, 0, identity, T["dim"], T["bar"], maxw=24)
    x = 14 + min(len(identity), 24) + 2
    for num, name, unread in pills:
        active = num == active_pill
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


def tabbar(s, title="≡ Gelen Kutusu"):
    s.fill(0, 1, W, 1, T["bar"])
    tw = max(22, len(title) + 8)
    s.fill(1, 1, tw, 1, T["selfocus"])
    s.put(1, 1, "▌", T["accent"], T["selfocus"])
    s.put(3, 1, "1", T["accent"], T["selfocus"], bold=True)
    s.put(5, 1, title, T["fg"], T["selfocus"], bold=True)


def folder_footer(s):
    s.put(1, TOP + PH - 4, "─" * (LW - 2), T["border"])
    s.put(2, TOP + PH - 3, "⊕ Engellenenler", T["dim"])
    s.rput(LW - 2, TOP + PH - 3, "Ctrl+B", fg=T["faint"])
    s.put(2, TOP + PH - 2, "+ Hesap ekle", T["accent"])
    s.rput(LW - 2, TOP + PH - 2, "+", fg=T["faint"])


def folders(s, focused=False):
    """Current app folder panel (used by search & grouping mockups)."""
    s.box(0, TOP, LW, PH, fg=T["borderf"] if focused else T["border"],
          bg=T["panel"], title="Klasörler")
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
    folder_footer(s)


def account_tree(s, focused=True, expand="none", cursor="all"):
    """Proposed left panel: accounts as collapsible address rows.
    expand: 'none' | 'gmail'
    cursor: 'all' | 'gmail-inbox'
    """
    border = T["borderf"] if focused else T["border"]
    s.box(0, TOP, LW, PH, fg=border, bg=T["panel"], title="Klasörler")
    y = TOP + 2

    def mark(y, active, icon=None):
        if active:
            bg = T["selfocus"] if focused else T["sel"]
            s.fill(1, y, LW - 2, 1, bg)
            s.put(2, y, "▌", T["accent"], bg)
            return bg
        if icon:
            s.put(2, y, icon, T["dim"] if icon == "▸" else T["fg"])
        return None

    def count_at(y, n, bg, yellow=False, active=False):
        if n is None or n == "" or n == 0:
            return
        s.rput(LW - 2, y, str(n),
               fg=T["yellow"] if (yellow or active) else T["faint"],
               bg=bg, bold=active or yellow)

    # Tüm Gelen Kutuları
    active = cursor == "all"
    bg = mark(y, active, icon="≡")
    if not active:
        s.put(2, y, "≡", T["dim"])
    s.put(4, y, "Tüm Gelen Kutuları", T["fg"] if active else T["dim"], bg, bold=True)
    count_at(y, 10, bg, yellow=True, active=active)
    y += 1

    # Gmail account
    gmail_open = expand == "gmail"
    active = cursor == "gmail"
    bg = mark(y, active, icon="▾" if gmail_open else "▸")
    if not active:
        s.put(2, y, "▾" if gmail_open else "▸", T["fg"] if gmail_open else T["dim"])
    # address fits: ahmet@gmail.com = 16, at x=4 ends 19; count at 26
    s.put(4, y, "ahmet@gmail.com", T["fg"] if (active or gmail_open) else T["dim"],
          bg, bold=True, maxw=19)
    count_at(y, 8, bg, yellow=True, active=active)
    y += 1

    if gmail_open:
        folders_g = [
            ("Gelen Kutusu", 8, cursor == "gmail-inbox"),
            ("Kaydedilenler", 3, cursor == "gmail-saved"),
            ("Gönderilenler", 0, False),
            ("Taslaklar", 1, False),
            ("Spam", 0, False),
            ("Çöp Kutusu", 0, False),
        ]
        for name, n, active in folders_g:
            bg = mark(y, active)
            s.put(6, y, name, T["fg"] if active else T["dim"], bg, bold=active, maxw=16)
            if n:
                count_at(y, n, bg, yellow=(name == "Gelen Kutusu" or active), active=active)
            y += 1

    # Outlook account (always collapsed in these two shots)
    active = cursor == "outlook"
    bg = mark(y, active, icon="▸")
    if not active:
        s.put(2, y, "▸", T["dim"])
    s.put(4, y, "ahmet@outlook.com", T["fg"] if active else T["dim"],
          bg, bold=active, maxw=19)
    count_at(y, 2, bg, yellow=True, active=active)
    y += 1

    folder_footer(s)


def list_box(s, title, focused=True):
    s.box(LW, TOP, MW, PH,
          fg=T["borderf"] if focused else T["border"],
          bg=T["bg"], title=title)


def read_box(s, title="Okuma"):
    s.box(LW + MW, TOP, RW, PH, fg=T["border"], bg=T["bg"], title=title)


def reader(s, hdr, body, footer="Enter: iletiye gir  ·  o: sekmede aç", highlight=None):
    y = TOP + 1
    for k, v in hdr:
        s.put(RX, y, f"{k:<7}", T["faint"])
        val_fg = T["purple"] if k == "Ek" else T["fg"]
        if highlight:
            put_hl(s, RX + 8, y, v, highlight, val_fg, bold=(k == "Konu"), maxw=RWI - 8)
        else:
            s.put(RX + 8, y, v, val_fg, bold=(k == "Konu"), maxw=RWI - 8)
        y += 1
    s.put(LW + MW + 1, y, "─" * (RW - 2), T["border"])
    y += 2
    for line in body:
        if highlight:
            put_hl(s, RX, y, line, highlight, T["fg"], maxw=RWI)
        else:
            s.put(RX, y, line, T["fg"] if line.strip() else None, maxw=RWI)
        y += 1
    s.put(RX, TOP + PH - 2, footer, T["faint"], maxw=RWI)


def chip(s, x, y, text, on=False):
    if on:
        s.put(x, y, " " + text + " ", T["bg"], T["accent"], bold=True)
    else:
        s.put(x, y, " " + text + " ", T["fg"], T["raised"])
    return x + len(text) + 2


def search_bar(s, query, scope, cursor=True):
    y = TOP + 1
    s.fill(LW + 1, y, MW - 2, 1, T["selfocus"])
    bg = T["selfocus"]
    s.put(MX, y, "Ara", T["accent"], bg, bold=True)
    qx = MX + 4
    s.put(qx, y, query, T["fg"], bg, bold=True)
    if cursor:
        s.put(qx + len(query), y, "█", T["accent"], bg)
    lab = " " + scope + " "
    s.rput(MX + MWI, y, lab, fg=T["bg"], bg=T["accent"], bold=True)
    return y


def mail_mehmet():
    return (
        [("Kimden", "Mehmet Kaya <mehmet.kaya@gmail.com>"),
         ("Kime", "ahmet.yilmaz@gmail.com"),
         ("Tarih", "14 Kasım 2026, 14:31"),
         ("Konu", "Akşam yemeği?")],
        ["Bu akşam 20:00'de Karaköy'de buluşalım mı?",
         "",
         "İstersen bir yer öner, ben ayarlarım.",
         "",
         "Mehmet"],
    )


def mail_turkcell():
    return (
        [("Kimden", "Turkcell <fatura@turkcell.com.tr>"),
         ("Kime", "ahmet.yilmaz@gmail.com"),
         ("Tarih", "14 Kasım 2026, 13:05"),
         ("Konu", "Kasım 2026 faturanız hazır"),
         ("Ek", "▪ fatura_kasim_2026.pdf  (212 KB)")],
        ["Sayın Ahmet Yılmaz,",
         "",
         "0532 *** ** 47 numaralı hattınıza ait Kasım 2026",
         "faturanız oluşturulmuştur.",
         "",
         "  Fatura tutarı      :  349,90 TL",
         "  Son ödeme tarihi   :  28 Kasım 2026",
         "  Fatura dönemi      :  01.11.2026 – 30.11.2026",
         "",
         "Faturanızı ekteki dosyadan inceleyebilir, otomatik",
         "ödeme talimatı verdiyseniz herhangi bir işlem",
         "yapmanıza gerek yoktur.",
         "",
         "Bizi tercih ettiğiniz için teşekkür ederiz.",
         "",
         "Turkcell Müşteri Hizmetleri"],
    )


def mail_ayse_is():
    return (
        [("Kimden", "Ayşe Demir <ayse.demir@firma.com>"),
         ("Kime", "ahmet.yilmaz@firma.com"),
         ("Tarih", "14 Kasım 2026, 14:30"),
         ("Konu", "Proje toplantısı – yeni saat"),
         ("Ek", "▪ toplanti_notlari.pdf  (84 KB)")],
        ["Merhaba Ahmet,",
         "",
         "Toplantıyı bugün 16:00'ya aldık. Güncel ajandayı ve",
         "sunumu ekte gönderiyorum, bir bakabilirsen sevinirim.",
         "",
         "Görüşmek üzere,",
         "Ayşe Demir"],
    )


def mail_google():
    return (
        [("Kimden", "Google <bildirim@google.com>"),
         ("Kime", "ahmet.yilmaz@gmail.com"),
         ("Tarih", "14 Kasım 2026, 12:11"),
         ("Konu", "Güvenlik uyarısı: yeni oturum")],
        ["Hesabınızda yeni bir oturum açıldı.",
         "",
         "  Tarih    :  14 Kasım 2026, 12:09",
         "  Yer      :  İstanbul, Türkiye",
         "  Cihaz    :  Windows bilgisayar",
         "",
         "Bu sizseniz bir şey yapmanız gerekmez.",
         "Siz değilseniz şifrenizi hemen değiştirin.",
         "",
         "Google Hesap Bildirimleri"],
    )


def search_keybar(s):
    s.keybar(H - 1, [
        ("↑↓", "Gezin"),
        ("Enter", "Aç"),
        ("Tab", "Kapsam"),
        ("Esc", "Kapat"),
    ], right="? Yardım")


def group_keybar(s):
    s.keybar(H - 1, [
        ("↑↓", "Gezin"),
        ("Enter", "Aç"),
        ("→", "Genişlet"),
        ("←", "Daralt"),
        ("G", "Grupla"),
        ("n", "Yeni"),
        ("r", "Yanıtla"),
        ("k", "Kaydet"),
        ("d", "Sil"),
    ], right="? Yardım")


def tree_keybar(s):
    s.keybar(H - 1, [
        ("↑↓", "Gezin"),
        ("Enter", "Aç"),
        ("→", "Genişlet"),
        ("←", "Daralt"),
        ("+", "Hesap ekle"),
    ], right="? Yardım")


# ---------------------------------------------------------------------------
# 1. Empty search + recent searches
# ---------------------------------------------------------------------------
def shot_arama1():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s)
    list_box(s, "Arama · Kişisel · Gelen Kutusu")
    read_box(s)
    search_bar(s, "", "Bu klasör")
    y = TOP + 3
    s.put(MX, y, "Son aramalar", T["faint"]); y += 1
    recents = [
        "fatura",
        "kimden:ayşe",
        "toplantı notları",
        "ekli bu hafta",
        "okunmamış",
    ]
    for q in recents:
        s.put(MX, y, "·", T["faint"])
        s.put(MX + 2, y, q, T["fg"])
        y += 1
    y += 1
    s.put(MX, y, "Filtreler", T["faint"]); y += 1
    x = MX
    for label in ("kimden:", "ekli", "okunmamış", "kaydedilen"):
        x = chip(s, x, y, label, on=False) + 1
    y += 1
    x = MX
    for label in ("bugün", "bu hafta"):
        x = chip(s, x, y, label, on=False) + 1
    y += 2
    s.put(MX, y, "Yazmaya başlayın; sonuçlar anında görünür.", T["faint"], maxw=MWI)
    hdr, body = mail_mehmet()
    reader(s, hdr, body)
    search_keybar(s)
    return s


# ---------------------------------------------------------------------------
# 2. Live results for 'fatura'
# ---------------------------------------------------------------------------
def shot_arama2():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s)
    list_box(s, "Arama · Kişisel · Gelen Kutusu")
    read_box(s)
    search_bar(s, "fatura", "Bu klasör")
    y = TOP + 2
    s.put(MX, y, "5 sonuç", T["dim"])
    s.put(MX + 9, y, "·  sunucuda aranıyor…", T["faint"])
    results = [
        (True,  True,  "Turkcell",        "Kasım 2026 faturanız hazır",   "13:05", True,
         "…hattınıza ait Kasım 2026 faturanız oluşturulmuştur."),
        (False, False, "İstanbul İl Am.", "Ynt: Kasım ayı faturanız",     "Dün",   True,
         "…ekindeki fatura dökümünü onaylamanızı rica ederiz."),
        (False, False, "Banka",           "Ekstre ve fatura bildirimi",   "11 Kas",True,
         "…aylık fatura ve hesap hareketleriniz ekte."),
        (False, True,  "Site Yönetimi",   "Aidat faturası ektedir",       "Dün",   True,
         "…kasım ayı aidat faturanızı ekte bulabilirsiniz."),
        (False, False, "Enerjisa",        "Aralık fatura dönemi başladı", "10 Kas",False,
         "…yeni dönem faturanız 1 Aralık'ta kesilecektir."),
    ]
    needles = ["fatura"]
    y = TOP + 3
    s.put(LW + 1, y, "─" * (MW - 2), T["border"]); y += 1
    for sel, unr, frm, sub, date, att, snip in results:
        bg = T["selfocus"] if sel else None
        if bg:
            s.fill(LW + 1, y, MW - 2, 1, bg)
            s.fill(LW + 1, y + 1, MW - 2, 1, bg)
        s.put(MX, y, "●" if unr else " ", T["accent"], bg)
        put_hl(s, MX + 2, y, frm, needles, T["fg"] if unr else T["dim"], bg, bold=unr, maxw=15)
        put_hl(s, MX + 18, y, sub, needles, T["fg"] if unr else T["dim"], bg, bold=unr and sel, maxw=24)
        s.put(MX + 43, y, "▪" if att else " ", T["purple"], bg)
        s.rput(MX + MWI, y, date, fg=T["fg"] if unr else T["dim"], bg=bg)
        y += 1
        put_hl(s, MX + 2, y, snip, needles, T["faint"], bg, maxw=MWI - 2)
        y += 2  # blank line between results
    hdr, body = mail_turkcell()
    reader(s, hdr, body, highlight=needles)
    search_keybar(s)
    return s


# ---------------------------------------------------------------------------
# 3. Filter chips + all-accounts scope
# ---------------------------------------------------------------------------
def shot_arama3():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s)
    list_box(s, "Arama · tüm hesaplar")
    read_box(s)
    search_bar(s, "kimden:ayşe ekli bu hafta", "Tüm hesaplar")
    y = TOP + 2
    x = MX
    x = chip(s, x, y, "Kimden: Ayşe", on=True) + 1
    x = chip(s, x, y, "Ekli", on=True) + 1
    x = chip(s, x, y, "Bu hafta", on=True)
    y += 1
    s.put(MX, y, "3 sonuç", T["dim"])
    results = [
        (True,  True,  "İş",      "Ayşe Demir", "Proje toplantısı – yeni saat", "14:30", True,
         "…ajandayı ve sunumu ekte gönderiyorum…"),
        (False, False, "Kişisel", "Ayşe Demir", "Re: Hafta sonu planı",         "Dün",   True,
         "…fotoğrafları ekte gönderiyorum, bir bak."),
        (False, True,  "İş",      "Ayşe Demir", "İlt: Toplantı notları",        "12 Kas",True,
         "…toplantı notlarının son hali ektedir."),
    ]
    needles = ["ayşe"]
    y += 1
    s.put(LW + 1, y, "─" * (MW - 2), T["border"]); y += 1
    for sel, unr, acct, frm, sub, date, att, snip in results:
        bg = T["selfocus"] if sel else None
        if bg:
            s.fill(LW + 1, y, MW - 2, 1, bg)
            s.fill(LW + 1, y + 1, MW - 2, 1, bg)
        s.put(MX, y, "●" if unr else " ", T["accent"], bg)
        s.put(MX + 2, y, f"{acct:<8}", ACCT_FG[acct], bg, bold=True)
        put_hl(s, MX + 11, y, frm, needles, T["fg"] if unr else T["dim"], bg, bold=unr, maxw=11)
        s.put(MX + 23, y, sub, T["fg"] if unr else T["dim"], bg, bold=unr and sel, maxw=18)
        s.put(MX + 43, y, "▪" if att else " ", T["purple"], bg)
        s.rput(MX + MWI, y, date, fg=T["fg"] if unr else T["dim"], bg=bg)
        y += 1
        s.put(MX + 11, y, snip, T["faint"], bg, maxw=MWI - 11)
        y += 2
    hdr, body = mail_ayse_is()
    reader(s, hdr, body, highlight=needles)
    search_keybar(s)
    return s


# ---------------------------------------------------------------------------
# Grouped-list helpers
# ---------------------------------------------------------------------------
def placeholder_search(s, count_line):
    s.fill(MX, TOP + 1, MWI, 1, T["raised"])
    s.put(MX + 1, TOP + 1, "Ara: ", T["faint"], T["raised"])
    s.put(MX + 6, TOP + 1, "gönderen, konu veya içerik…", T["faint"], T["raised"])
    s.rput(MX + MWI - 1, TOP + 1, "/", fg=T["dim"], bg=T["raised"])
    s.put(MX, TOP + 2, count_line, T["faint"])


def group_header(s, y, name, count, badge, date, sel=False, open_=False):
    bg = T["selfocus"] if sel else T["raised"]
    s.fill(LW + 1, y, MW - 2, 1, bg)
    arrow = "▾" if open_ else "▸"
    s.put(MX, y, arrow, T["accent"] if sel or open_ else T["dim"], bg)
    s.put(MX + 2, y, name, T["fg"], bg, bold=True)
    s.put(MX + 2 + len(name), y, f" ({count})", T["faint"], bg)
    s.rput(MX + MWI, y, date, fg=T["dim"], bg=bg)
    if badge:
        s.rput(MX + MWI - len(date) - 2, y, badge, fg=T["yellow"], bg=bg, bold=True)
    return y + 1


def single_mail(s, y, unr, frm, sub, date, att=False, sel=False, indent=0, acct=None):
    bg = T["selfocus"] if sel else None
    if bg:
        s.fill(LW + 1, y, MW - 2, 1, bg)
    x0 = MX + indent
    s.put(x0, y, "●" if unr else " ", T["accent"], bg)
    col = x0 + 2
    if acct:
        s.put(col, y, f"{acct:<8}", ACCT_FG[acct], bg, bold=True)
        col += 9
        s.put(col, y, frm, T["fg"] if unr else T["dim"], bg, bold=unr, maxw=12)
        sub_x = col + 13
    else:
        s.put(col, y, frm, T["fg"] if unr else T["dim"], bg, bold=unr, maxw=14)
        sub_x = x0 + 17
    att_x = MX + 43
    if sub_x < att_x:
        s.put(sub_x, y, sub, T["fg"] if unr else T["dim"], bg, bold=unr and sel,
              maxw=max(4, att_x - sub_x - 1))
    s.put(att_x, y, "▪" if att else " ", T["purple"], bg)
    s.rput(MX + MWI, y, date, fg=T["fg"] if unr else T["dim"], bg=bg)
    return y + 1


# ---------------------------------------------------------------------------
# 4. Grouped view, all collapsed
# ---------------------------------------------------------------------------
def shot_grup1():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s)
    list_box(s, "Kişisel · Gelen Kutusu")
    read_box(s)
    placeholder_search(s, "25 ileti · 8 okunmamış")
    y = TOP + 3
    s.put(LW + 1, y, "─" * (MW - 2), T["border"]); y += 1
    y = single_mail(s, y, True,  "Mehmet Kaya",     "Akşam yemeği?",                  "14:31", sel=True)
    y = group_header(s, y, "Google", 5, "● 3 yeni", "13:14")
    y = single_mail(s, y, True,  "Turkcell",        "Kasım 2026 faturanız hazır",     "13:05", att=True)
    y = group_header(s, y, "GitHub", 3, "● 1 yeni", "11:48")
    y = single_mail(s, y, True,  "Site Yönetimi",   "Aidat ödemeniz alındı",          "06:33")
    y = group_header(s, y, "Ayşe Demir", 3, "● 1 yeni", "Dün")
    y = single_mail(s, y, False, "İstanbul İl Am.","Ynt: Kasım ayı faturanız",       "Dün", att=True)
    y = group_header(s, y, "Trendyol", 3, None, "Dün")
    y = single_mail(s, y, True,  "Muhasebe",        "Ekim ayı ekstresi (PDF)",        "Dün", att=True)
    y = single_mail(s, y, False, "Zeynep Arslan",   "Fotoğraflar",                    "12 Kas", att=True)
    y = group_header(s, y, "Banka", 2, None, "11 Kas")
    y = single_mail(s, y, False, "Spotify",         "Bu haftanın keşifleri",          "10 Kas")
    y = group_header(s, y, "Udemy", 2, None, "8 Kas")
    y = single_mail(s, y, False, "Burak Yıldız",    "Sunum dosyası",                  "7 Kas", att=True)
    y = single_mail(s, y, False, "LinkedIn",        "Profilinizi 12 kişi görüntüledi","7 Kas")
    y = single_mail(s, y, False, "Selin Aydın",     "Teşekkürler!",                   "6 Kas")
    y = single_mail(s, y, False, "Hepsiburada",     "Favorinizde indirim var",        "5 Kas")
    hdr, body = mail_mehmet()
    reader(s, hdr, body)
    group_keybar(s)
    return s


# ---------------------------------------------------------------------------
# 5. One group expanded (Google), a mail selected, badge cleared
# ---------------------------------------------------------------------------
def shot_grup2():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s)
    list_box(s, "Kişisel · Gelen Kutusu")
    read_box(s)
    placeholder_search(s, "25 ileti · 8 okunmamış")
    y = TOP + 3
    s.put(LW + 1, y, "─" * (MW - 2), T["border"]); y += 1
    y = single_mail(s, y, True,  "Mehmet Kaya",     "Akşam yemeği?",                  "14:31")
    y = group_header(s, y, "Google", 5, None, "13:14", open_=True)
    y = single_mail(s, y, True,  "Google Play",     "Google Play'de satın alma onayı","13:14", indent=4)
    y = single_mail(s, y, True,  "Google",          "Güvenlik uyarısı: yeni oturum",  "12:11", indent=4, sel=True)
    y = single_mail(s, y, True,  "Google",          "Fotoğraflarınız yedeklendi",     "12 Kas", indent=4)
    y = single_mail(s, y, False, "Google",          "Hesap etkinliği özeti",          "10 Kas", indent=4)
    y = single_mail(s, y, False, "Google",          "Yeni özellik: hızlı paylaşım",   "8 Kas", indent=4)
    y = single_mail(s, y, True,  "Turkcell",        "Kasım 2026 faturanız hazır",     "13:05", att=True)
    y = group_header(s, y, "GitHub", 3, "● 1 yeni", "11:48")
    y = single_mail(s, y, True,  "Site Yönetimi",   "Aidat ödemeniz alındı",          "06:33")
    y = group_header(s, y, "Ayşe Demir", 3, "● 1 yeni", "Dün")
    y = single_mail(s, y, False, "İstanbul İl Am.","Ynt: Kasım ayı faturanız",       "Dün", att=True)
    y = group_header(s, y, "Trendyol", 3, None, "Dün")
    y = single_mail(s, y, True,  "Muhasebe",        "Ekim ayı ekstresi (PDF)",        "Dün", att=True)
    y = single_mail(s, y, False, "Zeynep Arslan",   "Fotoğraflar",                    "12 Kas", att=True)
    y = group_header(s, y, "Banka", 2, None, "11 Kas")
    hdr, body = mail_google()
    reader(s, hdr, body)
    group_keybar(s)
    return s


# ---------------------------------------------------------------------------
# 6. Account tree — all collapsed, Tüm Gelen Kutuları selected
# ---------------------------------------------------------------------------
TWO_PILLS = [(1, "Kişisel", 8), (2, "İş", 2)]


def shot_hesap1():
    s = Screen(W, H)
    header(s, identity="Tüm Gelen Kutuları", pills=TWO_PILLS, active_pill=0)
    tabbar(s, "≡ Tüm Gelen Kutuları")
    account_tree(s, focused=True, expand="none", cursor="all")
    list_box(s, "Tüm Gelen Kutuları", focused=False)
    read_box(s)
    placeholder_search(s, "28 ileti · 10 okunmamış")
    y = TOP + 3
    s.put(LW + 1, y, "─" * (MW - 2), T["border"]); y += 1
    rows = [
        (True,  True,  "Kişisel", "Mehmet Kaya",    "Akşam yemeği?",                  "14:31", False),
        (False, True,  "İş",      "Ayşe Demir",     "Proje toplantısı – yeni saat",   "14:30", True),
        (False, True,  "Kişisel", "Google",         "Güvenlik uyarısı: yeni oturum",  "13:14", False),
        (False, True,  "Kişisel", "Turkcell",       "Kasım 2026 faturanız hazır",     "13:05", True),
        (False, True,  "Kişisel", "GitHub",         "[notify-guard] v0.2 derleme",    "11:48", False),
        (False, False, "İş",      "Can Öztürk",     "Bütçe tablosu",                  "Dün",   True),
        (False, False, "Kişisel", "Ayşe Demir",     "Re: Hafta sonu planı",           "Dün",   False),
        (False, False, "Kişisel", "Trendyol",       "Siparişiniz kargoya verildi",    "Dün",   False),
        (False, True,  "İş",      "İK Departmanı",  "Yılbaşı izin planı",             "12 Kas",True),
        (False, False, "Kişisel", "Zeynep Arslan",  "Fotoğraflar",                    "12 Kas",True),
        (False, False, "Kişisel", "Banka",          "Ekstre bildirimi",               "11 Kas",True),
        (False, False, "Kişisel", "Spotify",        "Bu haftanın keşifleri",          "10 Kas",False),
        (False, False, "İş",      "Deniz Koç",      "Haftalık durum raporu",          "10 Kas",False),
        (False, False, "Kişisel", "Elif Şahin",     "Re: Doğum günü sürprizi",        "9 Kas", False),
        (False, False, "Kişisel", "Udemy",          "Kursunuza yeni bölüm eklendi",  "8 Kas", False),
        (False, False, "Kişisel", "Burak Yıldız",   "Sunum dosyası",                  "7 Kas", True),
        (False, False, "Kişisel", "LinkedIn",       "Profilinizi 12 kişi görüntüledi","7 Kas", False),
        (False, False, "Kişisel", "Selin Aydın",    "Teşekkürler!",                   "6 Kas", False),
        (False, False, "Kişisel", "Hepsiburada",    "Favorinizde indirim var",        "5 Kas", False),
    ]
    for sel, unr, acct, frm, sub, date, att in rows:
        y = single_mail(s, y, unr, frm, sub, date, att=att, sel=sel, acct=acct)
        if y >= TOP + PH - 1:
            break
    hdr, body = mail_mehmet()
    reader(s, hdr, body)
    tree_keybar(s)
    return s


# ---------------------------------------------------------------------------
# 7. Account tree — Gmail expanded, its Gelen Kutusu selected
# ---------------------------------------------------------------------------
def shot_hesap2():
    s = Screen(W, H)
    header(s, identity="ahmet.yilmaz@gmail.com", pills=TWO_PILLS, active_pill=1)
    tabbar(s, "≡ Gelen Kutusu")
    account_tree(s, focused=True, expand="gmail", cursor="gmail-inbox")
    list_box(s, "Kişisel · Gelen Kutusu", focused=False)
    read_box(s)
    placeholder_search(s, "25 ileti · 8 okunmamış")
    y = TOP + 3
    s.put(LW + 1, y, "─" * (MW - 2), T["border"]); y += 1
    rows = [
        (True,  True,  "Mehmet Kaya",    "Akşam yemeği?",                  "14:31", False),
        (False, True,  "Google",         "Güvenlik uyarısı: yeni oturum",  "13:14", False),
        (False, True,  "Turkcell",       "Kasım 2026 faturanız hazır",     "13:05", True),
        (False, True,  "GitHub",         "[notify-guard] v0.2 derleme",    "11:48", False),
        (False, True,  "Google Play",    "Google Play'de satın alma onayı","09:33", False),
        (False, True,  "Site Yönetimi",  "Aidat ödemeniz alındı",          "06:33", False),
        (False, False, "Ayşe Demir",     "Re: Hafta sonu planı",           "Dün",   False),
        (False, False, "İstanbul İl Am.","Ynt: Kasım ayı faturanız",       "Dün",   True),
        (False, False, "Trendyol",       "Siparişiniz kargoya verildi",    "Dün",   False),
        (False, True,  "Muhasebe",       "Ekim ayı ekstresi (PDF)",        "Dün",   True),
        (False, False, "Zeynep Arslan",  "Fotoğraflar",                    "12 Kas",True),
        (False, False, "Google",         "Fotoğraflarınız yedeklendi",     "12 Kas",False),
        (False, False, "Banka",          "Ekstre bildirimi",               "11 Kas",True),
        (False, False, "Spotify",        "Bu haftanın keşifleri",          "10 Kas",False),
        (False, False, "Elif Şahin",     "Re: Doğum günü sürprizi",        "9 Kas", False),
        (False, False, "Udemy",          "Kursunuza yeni bölüm eklendi",  "8 Kas", False),
        (False, False, "Burak Yıldız",   "Sunum dosyası",                  "7 Kas", True),
        (False, False, "LinkedIn",       "Profilinizi 12 kişi görüntüledi","7 Kas", False),
        (False, False, "Selin Aydın",    "Teşekkürler!",                   "6 Kas", False),
        (False, False, "Hepsiburada",    "Favorinizde indirim var",        "5 Kas", False),
    ]
    for sel, unr, frm, sub, date, att in rows:
        y = single_mail(s, y, unr, frm, sub, date, att=att, sel=sel)
        if y >= TOP + PH - 1:
            break
    hdr, body = mail_mehmet()
    reader(s, hdr, body)
    tree_keybar(s)
    return s


if __name__ == "__main__":
    jobs = [
        (shot_arama1, "arama-1-son-aramalar", "Termail"),
        (shot_arama2, "arama-2-canli-sonuc", "Termail"),
        (shot_arama3, "arama-3-filtre", "Termail"),
        (shot_grup1,  "grup-1-kapali", "Termail"),
        (shot_grup2,  "grup-2-acik", "Termail"),
        (shot_hesap1, "hesap-1-kapali", "Termail"),
        (shot_hesap2, "hesap-2-acik", "Termail"),
    ]
    for fn, name, title in jobs:
        s = fn()
        out = render(s, name, title)
        print(out)
