from tui import *
W,H=140,40
s=Screen(W,H)
s.fill(0,0,W,1,T["bar"])
s.put(1,0," ◆ Posta ",T["bg"],T["accent"],bold=True)
s.put(12,0,"Yanıt yazılıyor",T["dim"],T["bar"])
s.rput(W-1,0,"Çevrimiçi   14:36 ",fg=T["dim"],bg=T["bar"]); s.put(W-9,0,"●",T["green"],T["bar"])
tabbar(s,1,3)
top=2; ph=H-top-1
s.box(0,top,W,ph,fg=T["borderf"],title="Yanıtla · Proje toplantısı")
lx=3; fx=16; fw=W-fx-4
def field(y,label,val,focus=False,hint=None,valfg=None):
    s.put(lx,y,label,T["dim"] if not focus else T["accent"],bold=focus)
    s.fill(fx,y,fw,1,T["raised"] if focus else T["panel"])
    s.put(fx+1,y,val,valfg or T["fg"],T["raised"] if focus else T["panel"])
    if hint: s.rput(fx+fw-1,y,hint,fg=T["faint"],bg=T["raised"] if focus else T["panel"])
y=top+2
field(y,"Gönderen","◂  Kişisel  <ahmet.yilmaz@gmail.com>  ▸",focus=True,hint="Ctrl+F veya ← →: hesap değiştir"); gy=y; y+=2
field(y,"Kime","Ayşe Demir <ayse.demir@firma.com.tr>;  mehmet.kaya@gmail.com"); y+=2
field(y,"Bilgi","",hint="isteğe bağlı"); y+=2
field(y,"Konu","Re: Proje toplantısı – yeni saat"); y+=2
field(y,"Ekler","▪ sunum_taslak.pptx (1,4 MB)    ▪ butce.xlsx (86 KB)",valfg=T["purple"],hint="Ctrl+E: ek ekle"); y+=2
s.put(1,y,"─"*(W-2),T["border"]); s.put(0,y,"├",T["border"]); s.put(W-1,y,"┤",T["border"])
s.put(3,y," İleti ",T["accent"],bold=True); y+=2
body=["Merhaba Ayşe,","",
"Yeni saat benim için uygun, perşembe 15:00'te görüşelim.",
"Sunum taslağını ve güncel bütçe tablosunu ekte gönderiyorum;",
"toplantıdan önce göz atarsan sevinirim.","",
"Görüşmek üzere,","Ahmet",""]
for l in body: s.put(lx+1,y,l,T["fg"]); y+=1

y+=1
s.put(lx+1,y,"──── Ayşe Demir, 14 Kasım 2026 14:30 tarihinde yazdı: ────",T["faint"]); y+=1
for l in ["Merhaba Ahmet,","Toplantıyı perşembe 15:00'e almamız gerekiyor, uygun musun?","Ayşe"]:
    s.put(lx+1,y,"│ "+l,T["faint"]); y+=1
dx=fx; dy=gy+1; dw=52
s.box(dx,dy,dw,5,fg=T["borderf"],bg=T["raised"])
acc=[("Kişisel","ahmet.yilmaz@gmail.com",True),("İş","ahmet.yilmaz@firma.com.tr",False),("Okul","a.yilmaz@ogr.univ.edu.tr",False)]
for i,(n,e,a) in enumerate(acc):
    bg=T["selfocus"] if a else T["raised"]
    s.fill(dx+1,dy+1+i,dw-2,1,bg)
    s.put(dx+2,dy+1+i,"●" if a else "○",T["accent"],bg)
    s.put(dx+4,dy+1+i,f"{n:<8}",T["fg"],bg,bold=a); s.put(dx+13,dy+1+i,e,T["dim"] if not a else T["fg"],bg)
# status line inside box bottom
s.put(lx,top+ph-2,"Taslak otomatik kaydedildi · 14:36",T["faint"])
s.rput(W-3,top+ph-2,"58 kelime",fg=T["faint"])
s.keybar(H-1,[("Ctrl+Enter","Gönder"),("Tab","Sonraki alan"),("Ctrl+F","Gönderen hesabı"),("Ctrl+E","Ek ekle"),("Ctrl+Tab","Sekme"),("Esc","Taslağa kaydet")],right="? Yardım")
print(render(s,"03-eposta-yazma","Posta"))
