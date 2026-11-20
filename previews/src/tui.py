import html, subprocess, os
# Theme: anthracite (no pure black)
T = dict(
    bg="#1f2227", panel="#23272d", raised="#2a2f36", sel="#323a45", selfocus="#34465a",
    border="#3b414a", borderf="#5b7896", fg="#d5d8dd", dim="#8b929c", faint="#5f6670",
    accent="#7fa7cc", green="#93c08f", yellow="#d9bb72", red="#d48a86", purple="#b49ad6",
    bar="#191c20", key="#2e353e",
)
class Screen:
    def __init__(s, w, h):
        s.w, s.h = w, h
        s.c = [[[" ", T["fg"], T["bg"], False] for _ in range(w)] for _ in range(h)]
    def fill(s, x, y, w, h, bg, ch=" "):
        for yy in range(y, y+h):
            for xx in range(x, x+w):
                s.c[yy][xx] = [ch, T["fg"], bg, False]
    def put(s, x, y, text, fg=None, bg=None, bold=False, maxw=None):
        fg = fg or T["fg"]
        if maxw is not None and len(text) > maxw:
            text = text[:maxw-1] + "…"
        for i, ch in enumerate(text):
            xx = x+i
            if xx >= s.w: raise ValueError(f"overflow row {y}: {text!r}")
            cell = s.c[y][xx]
            cell[0] = ch; cell[1] = fg; cell[3] = bold
            if bg: cell[2] = bg
        return x+len(text)
    def rput(s, xr, y, text, **k):  # right-aligned ending at xr (exclusive)
        return s.put(xr-len(text), y, text, **k)
    def box(s, x, y, w, h, fg=None, bg=None, title=None, tfg=None):
        fg = fg or T["border"]
        if bg: s.fill(x, y, w, h, bg)
        s.put(x, y, "╭"+"─"*(w-2)+"╮", fg)
        s.put(x, y+h-1, "╰"+"─"*(w-2)+"╯", fg)
        for yy in range(y+1, y+h-1):
            s.put(x, yy, "│", fg); s.put(x+w-1, yy, "│", fg)
        if title:
            s.put(x+2, y, " "+title+" ", tfg or T["accent"], bold=True)
    def hline(s, x, y, w, fg=None, l="├", r="┤"):
        s.put(x, y, l+"─"*(w-2)+r, fg or T["border"])
    def keybar(s, y, items, right=None):
        s.fill(0, y, s.w, 1, T["bar"])
        x = 1
        for k, label in items:
            x = s.put(x, y, f" {k} ", T["fg"], T["key"], bold=True)
            x = s.put(x, y, f" {label}", T["dim"], T["bar"])
            x += 3
        if right: s.rput(s.w-1, y, right, fg=T["faint"], bg=T["bar"])
    def html(s, title, fontpx=17):
        rows = []
        for row in s.c:
            out = []; i = 0
            while i < len(row):
                j = i
                while j < len(row) and row[j][1:] == row[i][1:]: j += 1
                ch, fg, bg, b = row[i]
                txt = "".join(c[0] for c in row[i:j])
                # each glyph in fixed 1ch cell to guarantee grid
                cells = "".join(f"<i>{html.escape(c)}</i>" for c in txt)
                out.append(f'<span style="color:{fg};background:{bg};{"font-weight:700;" if b else ""}">{cells}</span>')
                i = j
            rows.append("<div>"+"".join(out)+"</div>")
        return f"""<!doctype html><html><head><meta charset=utf-8><style>
body{{margin:0;background:#15171a;padding:28px;}}
.win{{display:inline-block;border-radius:10px;overflow:hidden;box-shadow:0 18px 50px rgba(0,0,0,.55);border:1px solid #33383f}}
.tb{{background:#2b2f35;height:34px;display:flex;align-items:center;font:13px 'Segoe UI',sans-serif;color:#c9ccd1}}
.tab{{background:{T['bg']};height:34px;display:flex;align-items:center;padding:0 16px;border-right:1px solid #33383f}}
.ctl{{margin-left:auto;display:flex;gap:26px;padding-right:18px;color:#9aa0a8;font-size:14px}}
.term{{background:{T['bg']};padding:6px 8px;font-family:'Cascadia Code';font-size:{fontpx}px;line-height:1.2;font-variant-ligatures:none}}
.term div{{white-space:pre;height:1.2em}}
.term i{{font-style:normal;display:inline-block;width:1ch;text-align:center;overflow:visible}}
</style></head><body><div class=win><div class=tb><div class=tab>▸&nbsp;&nbsp;{html.escape(title)}</div><div style="padding:0 12px;color:#8a9099">+&nbsp;&nbsp;⌄</div><div class=ctl><span>—</span><span>☐</span><span>✕</span></div></div><div class=term>{''.join(rows)}</div></div></body></html>"""

def render(screen, name, title):
    d = os.path.dirname(os.path.abspath(__file__))
    hp = os.path.join(d, name+".html")
    open(hp, "w").write(screen.html(title))
    out = os.path.join(d, "..", name+".png")
    subprocess.run(["google-chrome","--headless=new","--no-sandbox","--disable-gpu","--hide-scrollbars",
        "--force-device-scale-factor=2",f"--window-size={int(screen.w*10.25+72)},{int(screen.h*20.4+100)}",f"--screenshot={out}", "file://"+hp],
        check=True, capture_output=True)
    return out

TABS_DEFAULT=[("≡","Gelen Kutusu",None),("","Kasım faturası",None),("","Proje toplantısı","unread"),("","Yanıt: Proje toplantısı","draft")]
def tabbar(s, y, active, tabs=TABS_DEFAULT, tw=25):
    s.fill(0,y,s.w,1,T["bar"])
    x=1
    for i,(icon,title,flag) in enumerate(tabs):
        a=(i==active); bg=T["selfocus"] if a else T["panel"]
        s.fill(x,y,tw,1,bg)
        s.put(x,y,"▌",T["accent"] if a else T["border"],bg)
        s.put(x+2,y,str(i+1),T["accent"] if a else T["faint"],bg,bold=a)
        tx=x+4
        if icon: s.put(tx,y,icon,T["fg"] if a else T["dim"],bg); tx+=2
        mark={"unread":("●",T["yellow"]),"draft":("◇",T["purple"])}.get(flag)
        room=tw-(tx-x)-(3 if mark else 1)
        s.put(tx,y,title,T["fg"] if a else T["dim"],bg,bold=a,maxw=room)
        if mark: s.put(x+tw-2,y,mark[0],mark[1],bg)
        x+=tw+1
    s.rput(s.w-1,y,"Alt+1-9 · Ctrl+W kapat ",fg=T["faint"],bg=T["bar"])
