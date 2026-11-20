#!/usr/bin/env python3
"""Render a tmux `capture-pane -e -p` dump (ANSI truecolor) to PNG via headless Chrome,
using the same window chrome as the approved previews."""
import html, re, subprocess, sys, os, unicodedata

DEF_FG, DEF_BG = "#d5d8dd", "#1f2227"

def parse(text, w, h):
    rows = []
    for line in text.split("\n")[:h]:
        cells = []
        fg, bg, bold = DEF_FG, DEF_BG, False
        i = 0
        while i < len(line):
            m = re.match(r"\x1b\[([0-9;:]*)m", line[i:])
            if m:
                ps = [p for p in m.group(1).replace(":", ";").split(";")]
                if ps == [""]:
                    ps = ["0"]
                k = 0
                while k < len(ps):
                    p = int(ps[k] or 0)
                    if p == 0: fg, bg, bold = DEF_FG, DEF_BG, False
                    elif p == 1: bold = True
                    elif p == 22: bold = False
                    elif p == 39: fg = DEF_FG
                    elif p == 49: bg = DEF_BG
                    elif p in (38, 48) and k + 4 < len(ps) and ps[k+1] == "2":
                        c = "#%02x%02x%02x" % (int(ps[k+2]), int(ps[k+3]), int(ps[k+4]))
                        if p == 38: fg = c
                        else: bg = c
                        k += 4
                    elif p in (38, 48) and k + 2 < len(ps) and ps[k+1] == "5":
                        k += 2
                    k += 1
                i += m.end()
                continue
            m = re.match(r"\x1b[^a-zA-Z]*[a-zA-Z]", line[i:])
            if m:
                i += m.end(); continue
            ch = line[i]
            wide = unicodedata.east_asian_width(ch) in "WF"
            cells.append((ch, fg, bg, bold, 2 if wide else 1))
            if wide:  # the terminal gives it two cells; tmux omits the second
                cells.append(None)
            i += 1
        while len(cells) < w:
            cells.append((" ", DEF_FG, DEF_BG, False, 1))
        rows.append([c for c in cells[:w] if c is not None])
    while len(rows) < h:
        rows.append([(" ", DEF_FG, DEF_BG, False, 1)] * w)
    return rows

def to_html(rows, title):
    out = []
    for row in rows:
        parts = []
        for ch, fg, bg, b, cw in row:
            parts.append(f'<i style="color:{fg};background:{bg};{"font-weight:700;" if b else ""}{"width:2ch;" if cw == 2 else ""}">{html.escape(ch)}</i>')
        out.append("<div>" + "".join(parts) + "</div>")
    return f"""<!doctype html><html><head><meta charset=utf-8><style>
body{{margin:0;background:#15171a;padding:28px;}}
.win{{display:inline-block;border-radius:10px;overflow:hidden;box-shadow:0 18px 50px rgba(0,0,0,.55);border:1px solid #33383f}}
.tb{{background:#2b2f35;height:34px;display:flex;align-items:center;font:13px 'Segoe UI',sans-serif;color:#c9ccd1}}
.tab{{background:{DEF_BG};height:34px;display:flex;align-items:center;padding:0 16px;border-right:1px solid #33383f}}
.ctl{{margin-left:auto;display:flex;gap:26px;padding-right:18px;color:#9aa0a8;font-size:14px}}
.term{{background:{DEF_BG};padding:6px 8px;font-family:'Cascadia Code','DejaVu Sans Mono',monospace;font-size:17px;line-height:1.2;font-variant-ligatures:none}}
.term div{{white-space:pre;height:1.2em}}
.term i{{font-style:normal;display:inline-block;width:1ch;text-align:center;overflow:visible}}
</style></head><body><div class=win><div class=tb><div class=tab>▸&nbsp;&nbsp;{html.escape(title)}</div><div style="padding:0 12px;color:#8a9099">+&nbsp;&nbsp;⌄</div><div class=ctl><span>—</span><span>☐</span><span>✕</span></div></div><div class=term>{''.join(out)}</div></div></body></html>"""

if __name__ == "__main__":
    src, out = sys.argv[1], sys.argv[2]
    w, h = int(sys.argv[3]) if len(sys.argv) > 3 else 140, int(sys.argv[4]) if len(sys.argv) > 4 else 40
    rows = parse(open(src, encoding="utf-8", errors="replace").read(), w, h)
    hp = out[:-4] + ".html"
    open(hp, "w").write(to_html(rows, "Termail"))
    subprocess.run(["google-chrome", "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
                    "--force-device-scale-factor=2", f"--window-size={int(w*10.25+72)},{int(h*20.4+100)}",
                    f"--screenshot={os.path.abspath(out)}", "file://" + os.path.abspath(hp)], check=True, capture_output=True)
    os.remove(hp)
    print(out)
