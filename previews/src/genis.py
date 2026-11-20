# -*- coding: utf-8 -*-
"""Preview mockups: wide/focus reading mode. Does not touch app source or termail.exe.

Key: z (plain z is free in README). f is taken — r/a/f = Yanıtla / Tümüne / İlet.
Ctrl+Z is only undo while composing.
"""
import sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tui import *
from ekler import (
    W, H, LW, MW, RW, TOP, PH, MX, MWI, RX, RWI, TIME,
    header, tabbar, folders, list_pane, reader_box, reader_tabs,
    BODY as EKLER_BODY,
)

# Same mail as ekler-1, plus a slightly longer closing paragraph so
# the comfortable ~96-col wrap is visible in wide mode (and wraps in the narrow pane).
EXTRA = [
    "Toplantıda son çeyrek hedeflerine ve ekteki bütçe",
    "tablosuna birlikte bakarız. Ofis krokisini yeni",
    "oturma düzeni için ekledim; bir bakıp kısa bir not",
    "düşersen çok iyi olur.",
]
# Insert before "Görüşmek üzere,"
BODY_NARROW = []
for line in EKLER_BODY:
    if line == "Görüşmek üzere,":
        BODY_NARROW += [""] + EXTRA + [""]
    BODY_NARROW.append(line)

# Wide-mode body: same text, reflowed for a comfortable column (96 cols).
BODY_WIDE_PARAS = [
    "Merhaba Ahmet,",
    "",
    "Yarınki toplantı için gereken dosyaları ekledim:",
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
    "Toplantıda son çeyrek hedeflerine ve ekteki bütçe tablosuna birlikte bakarız. Ofis krokisini yeni oturma düzeni için ekledim; bir bakıp kısa bir not düşersen çok iyi olur.",
    "",
    "Görüşmek üzere,",
    "Ayşe Demir",
]

COL_W = 96
COL_X = (W - COL_W) // 2  # 22


def wrap_line(text, width):
    if not text.strip():
        return [""]
    # keep explicit bullet / indented lines intact if they fit
    words = text.split()
    lead = ""
    if text.startswith("  "):
        lead = "  "
        if text.lstrip().startswith("•"):
            lead = "  •  "
            words = text.lstrip()[1:].split()
    lines, cur = [], lead
    for i, w in enumerate(words):
        piece = w if cur.endswith("  ") or cur == "" or cur == lead and not cur.strip() else " " + w
        # first word after lead
        if cur == lead and lead:
            cand = lead + w
        elif cur == "":
            cand = w
        else:
            cand = cur + (" " if not cur.endswith(" ") else "") + w
            if cur.endswith("  "):
                cand = cur + w
        if len(cand) <= width:
            cur = cand
        else:
            lines.append(cur)
            cur = w
    if cur or cur == "":
        lines.append(cur)
    return lines


def hint_on_title(s, text, box_x=0, box_w=W):
    # faint hint on the right of the box title row
    s.rput(box_x + box_w - 2, TOP, " " + text + " ", fg=T["faint"])


def write_headers(s, x, y, rule_x, rule_w):
    hdr = [
        ("Kimden", "Ayşe Demir <ayse.demir@firma.com>"),
        ("Kime",   "ahmet.yilmaz@gmail.com"),
        ("Tarih",  "14 Kasım 2026, 18:40"),
        ("Konu",   "Proje toplantısı – dosyalar"),
        ("Ek",     "3 dosya"),
    ]
    for k, v in hdr:
        s.put(x, y, f"{k:<7}", T["faint"])
        fg = T["purple"] if k == "Ek" else T["fg"]
        s.put(x + 8, y, v, fg, bold=(k == "Konu"), maxw=rule_w - 8)
        y += 1
    s.put(rule_x, y, "─" * rule_w, T["border"])
    return y + 2


# ---------------------------------------------------------------------------
# 1. Normal 3-panel, same mail, keybar shows z Geniş okuma
# ---------------------------------------------------------------------------
def shot_normal():
    s = Screen(W, H)
    header(s); tabbar(s); folders(s); list_pane(s)
    reader_box(s, title="Okuma")
    hint_on_title(s, "çift tık: geniş okuma", box_x=LW + MW, box_w=RW)
    y = reader_tabs(s, active="mail")
    y = write_headers(s, RX, y, LW + MW + 1, RW - 2)
    for line in BODY_NARROW:
        if y >= TOP + PH - 2:
            break
        s.put(RX, y, line, T["fg"] if line.strip() else None, maxw=RWI)
        y += 1
    s.put(RX, TOP + PH - 2, "Enter: yanıtla  ·  z: geniş okuma",
          T["faint"], maxw=RWI)
    s.keybar(H - 1, [
        ("Esc", "Listeye dön"),
        ("↑↓", "Kaydır"),
        ("Enter", "Yanıtla"),
        ("e", "Ekler ve bağlantılar"),
        ("z", "Geniş okuma"),
        ("k", "Kaydet"),
        ("d", "Sil"),
    ], right="? Yardım")
    return s


# ---------------------------------------------------------------------------
# 2. Wide / focus mode: folder+list hidden, centred ~96-col column
# ---------------------------------------------------------------------------
def shot_wide():
    s = Screen(W, H)
    header(s); tabbar(s)
    # full-width reader
    s.box(0, TOP, W, PH, fg=T["borderf"], bg=T["bg"], title="Okuma")
    hint_on_title(s, "çift tık: normal görünüm", box_x=0, box_w=W)

    # tab row, aligned to the reading column
    y = TOP + 1
    s.fill(1, y, W - 2, 1, T["panel"])
    t1 = " Mail "
    t2 = " Ekler ve Bağlantılar (3 · 5) "
    x = COL_X
    s.fill(x, y, len(t1), 1, T["selfocus"])
    s.put(x, y, "▌", T["accent"], T["selfocus"])
    s.put(x + 1, y, t1[1:], T["fg"], T["selfocus"], bold=True)
    s.put(x + len(t1) + 1, y, t2, T["dim"], T["panel"])
    s.put(1, y + 1, "─" * (W - 2), T["border"])
    y += 2

    y = write_headers(s, COL_X, y, COL_X, COL_W)
    for para in BODY_WIDE_PARAS:
        for line in wrap_line(para, COL_W):
            if y >= TOP + PH - 2:
                break
            s.put(COL_X, y, line, T["fg"] if line.strip() else None, maxw=COL_W)
            y += 1
        if y >= TOP + PH - 2:
            break
    s.put(COL_X, TOP + PH - 2,
          "Enter: yanıtla  ·  z veya Esc: normal görünüm  ·  e: ekler",
          T["faint"], maxw=COL_W)
    s.keybar(H - 1, [
        ("z", "Normal görünüm"),
        ("Esc", "Normal görünüm"),
        ("↑↓", "Kaydır"),
        ("Enter", "Yanıtla"),
        ("e", "Ekler ve bağlantılar"),
        ("k", "Kaydet"),
    ], right="? Yardım")
    return s


if __name__ == "__main__":
    for fn, name in (
        (shot_normal, "genis-1-normal"),
        (shot_wide,   "genis-2-genis"),
    ):
        print(render(fn(), name, "Termail"))
