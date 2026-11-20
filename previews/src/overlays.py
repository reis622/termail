import os, sys, runpy
os.environ["NORENDER"]="1"; sys.argv=["main.py","list"]
from tui import *
def base():
    g=runpy.run_path("main.py"); s=g["s"]
    for row in s.c:
        for c in row:
            c[1]="#555c66"; c[3]=False
            if c[2]!=T["bg"]: c[2]=T["bg"] if c[2]!=T["panel"] else T["panel"]
    return s
W,H=140,40
# ---------- single-screen login ----------
def login(ms):
    s=Screen(W,H)
    cw,ch=64,20; cx=(W-cw)//2; cy=(H-ch)//2-1
    s.put((W-9)//2,cy-3," ◆ Posta ",T["bg"],T["accent"],bold=True)
    s.box(cx,cy,cw,ch,fg=T["border"],bg=T["panel"])
    fx=cx+4; fw=cw-8; y=cy+2
    s.put(fx,y,"Hoş geldin",T["fg"],bold=True); y+=1
    s.put(fx,y,"E-posta adresini gir, yeter." if ms else "E-posta adresini ve şifreni gir, yeter.",T["dim"]); y+=2
    s.put(fx,y,"E-posta",T["dim"]); y+=1
    s.fill(fx,y,fw,1,T["bg"])
    s.put(fx+1,y,"ahmet.yilmaz@hotmail.com" if ms else "ahmet.yilmaz@gmail.com",T["fg"],T["bg"])
    s.rput(fx+fw-1,y,"Outlook ✓" if ms else "Gmail ✓",fg=T["green"],bg=T["bg"]); y+=2
    if not ms:
        s.put(fx,y,"Şifre",T["accent"],bold=True); y+=1
        s.fill(fx,y,fw,1,T["raised"]); s.put(fx+1,y,"•••• •••• •••• ••",T["fg"],T["raised"]); s.put(fx+19,y,"▌",T["accent"],T["raised"]); y+=2
        s.put(fx,y,"i",T["yellow"],bold=True); s.put(fx+2,y,"Gmail'de normal şifre yerine uygulama şifresi gir.",T["dim"]); y+=1
        s.put(fx+2,y,"Nasıl alınır?  Ctrl+O     Google ile giriş:  Ctrl+G",T["accent"]); y+=2
        s.put(fx,y,"■",T["green"]); s.put(fx+2,y,"Şifren Windows'ta güvenle saklanır, bir daha sorulmaz.",T["faint"]); y+=2
        bt="  Enter  ·  Giriş yap  "
        keys=[("Enter","Giriş"),("Tab","Sonraki alan"),("Ctrl+O","Uygulama şifresi nasıl alınır"),("Ctrl+G","Google ile giriş"),("Esc","Çıkış")]
    else:
        s.put(fx,y,"Microsoft hesabı için şifre gerekmez.",T["fg"]); y+=1
        s.put(fx,y,"Tarayıcıda bir kez onay vermen yeterli.",T["dim"]); y+=2
        s.box(fx,y,fw,3,fg="#4f6a86"); s.fill(fx+1,y+1,fw-2,1,"#263241")
        s.put(fx+2,y+1,"●",T["accent"],"#263241"); s.put(fx+4,y+1,"Tarayıcıda onay bekleniyor…",T["fg"],"#263241")
        s.rput(fx+fw-2,y+1,"0:42",fg=T["faint"],bg="#263241"); y+=4
        s.put(fx,y,"■",T["green"]); s.put(fx+2,y,"Giriş Windows'ta güvenle saklanır, bir daha sorulmaz.",T["faint"]); y+=2
        bt="  Enter  ·  Tarayıcıda Microsoft ile giriş yap  "
        keys=[("Enter","Tarayıcıda giriş yap"),("Ctrl+L","Bağlantıyı kopyala"),("Esc","Geri")]
    s.put(cx+(cw-len(bt))//2,y,bt,T["bg"],T["accent"],bold=True)
    s.put((W-56)//2,cy+ch+1,"Sunucu ayarları otomatik bulunur; elle girmen gerekmez.",T["faint"])
    s.keybar(H-1,keys)
    return s
print(render(login(False),"04-giris","Posta"))
print(render(login(True),"04b-giris-microsoft","Posta"))
# ---------- help ----------
s=base()
dw,dh=120,22; dx=(W-dw)//2; dy=(H-dh)//2-1
s.box(dx,dy,dw,dh,fg=T["borderf"],bg=T["panel"],title="Kısayollar")
s.rput(dx+dw-3,dy," ? veya Esc: kapat ",fg=T["dim"])
cols=[
 [("Gezinme",[("↑ ↓","Yukarı / aşağı"),("Enter","İletiyi aç"),("Esc","Listeye dön"),("←","Klasör paneli"),("→","Listeye dön"),("/","Ara")]),
  ("Sekmeler",[("Tab","Sonraki sekme"),("Shift+Tab","Önceki sekme"),("Alt+1-9","Sekmeye git"),("Ctrl+W","Sekmeyi kapat")])],
 [("İleti",[("n","Yeni ileti"),("r","Yanıtla"),("a / R","Tümüne yanıtla"),("f","İlet"),("s","Yıldızla"),("e","Arşivle"),("d / Delete","Çöpe at"),("u","Silmeyi geri al"),("o","Okundu / okunmadı"),("m","Klasöre taşı")])],
 [("Yazarken",[("Ctrl+Enter","Gönder"),("Tab","Sonraki alan"),("Shift+Tab","Önceki alan"),("Ctrl+F","Gönderen hesabı"),("Ctrl+E","Ek ekle"),("Ctrl+Tab","Sekme değiştir"),("Esc","Taslağa kaydet")]),
  ("Genel",[("+","Hesap ekle"),("?","Bu ekran"),("q","Çıkış")])],
]
cw=(dw-6)//3
for ci,groups in enumerate(cols):
    x=dx+3+ci*(cw+1); y=dy+2
    for head,items in groups:
        s.put(x,y,head,T["accent"],bold=True); y+=1
        s.put(x,y,"─"*(cw-2),T["border"]); y+=1
        for k,v in items:
            s.put(x,y,f" {k} ",T["fg"],T["key"],bold=True); s.put(x+14,y,v,T["dim"],maxw=cw-15)
            y+=1
        y+=1
s.keybar(H-1,[("?","Kapat"),("Esc","Kapat")])
print(render(s,"05-kisayollar","Posta"))
