from tui import *
import os
if os.environ.get('NORENDER'):
    render=lambda s,n,t: ''
import sys
W,H=140,40
s=Screen(W,H)
# header
s.fill(0,0,W,1,T["bar"])
s.put(1,0," ◆ Posta ",T["bg"],T["accent"],bold=True)
s.put(12,0,"Kişisel · ahmet.yilmaz@gmail.com",T["dim"],T["bar"])
x=s.rput(W-1,0,"Çevrimiçi   14:32 ",fg=T["dim"],bg=T["bar"])
s.put(W-9,0,"●",T["green"],T["bar"])
tabbar(s,1,0)
# panes
top=2; ph=H-top-1
LW,MW=28,56; RW=W-LW-MW

s.box(LW+MW,top,RW,ph,bg=T["bg"],title="Okuma")
LF = (len(sys.argv)>1 and sys.argv[1]=="folders")
s.box(0,top,LW,ph,fg=T["borderf"] if LF else None,bg=T["panel"],title="Hesaplar ve Klasörler" if LF else "Hesaplar")
s.box(LW,top,MW,ph,fg=None if LF else T["borderf"],bg=T["bg"],title="Kişisel · Gelen Kutusu")
# left content
import sys
MODE=sys.argv[1] if len(sys.argv)>1 else "list"
y=top+2
def acct(y,name,kind,unread,open_):
    arrow="▾" if open_ else "▸"
    s.put(2,y,f"{arrow} {name}",T["fg"] if open_ else T["dim"],bold=open_)
    s.put(4,y+1,kind,T["faint"])
    if unread: s.rput(LW-2,y,str(unread),fg=T["yellow"],bold=True)
def folder(y,f,n,active,cursor):
    if cursor:
        s.fill(1,y,LW-2,1,T["selfocus"]); bg=T["selfocus"]
    elif active:
        s.fill(1,y,LW-2,1,T["sel"]); bg=T["sel"]
    else: bg=None
    if active: s.put(2,y,"▌",T["accent"],bg)
    s.put(4,y,f,T["fg"] if (active or cursor) else T["dim"],bg,bold=active or cursor)
    if n: s.rput(LW-3,y,str(n),fg=T["yellow"] if (active or cursor) else T["faint"],bg=bg,bold=active)
std=[("Gelen Kutusu",3),("Yıldızlı",0),("Gönderilenler",0),("Taslaklar",1),("Arşiv",0),("Spam",0),("Çöp Kutusu",0)]
if MODE=="list":
    acct(y,"Kişisel","Gmail",3,True); y+=2
    for i,(f,n) in enumerate(std): folder(y,f,n,i==0,False); y+=1
    y+=1; s.put(1,y,"─"*(LW-2),T["border"]); y+=2
    acct(y,"İş","Outlook",5,False); y+=3
    acct(y,"Okul","Diğer (IMAP)",0,False); y+=3
else:
    acct(y,"Kişisel","Gmail",3,True); y+=2
    for i,(f,n) in enumerate(std): folder(y,f,n,i==0,False); y+=1
    s.put(4,y,"Klasörlerim",T["faint"]); y+=1
    for f in ["Aile","Faturalar"]: folder(y,"  "+f,0,False,False); y+=1
    s.put(1,y,"─"*(LW-2),T["border"]); y+=1
    acct(y,"İş","Outlook",5,True); y+=2
    iw=[("Gelen Kutusu",4),("Gönderilenler",0),("Taslaklar",0),("Spam",0),("Çöp Kutusu",0)]
    for f,n in iw: folder(y,f,n,False,False); y+=1
    s.put(4,y,"Klasörlerim",T["faint"]); y+=1
    folder(y,"  Projeler",1,False,True); y+=1
    folder(y,"  Müşteriler",0,False,False); y+=1
    s.put(1,y,"─"*(LW-2),T["border"]); y+=1
    acct(y,"Okul","Diğer (IMAP)",0,False); y+=2
s.put(1,top+ph-4,"─"*(LW-2),T["border"])
s.put(2,top+ph-3,"+ Hesap ekle",T["accent"])
s.put(2,top+ph-2,"  (+ tuşu)",T["faint"])
# middle
mx=LW+2; mw=MW-4
s.fill(mx,top+1,mw,1,T["raised"])
s.put(mx+1,top+1,"Ara: ",T["faint"],T["raised"]); s.put(mx+6,top+1,"gönderen, konu veya içerik…",T["faint"],T["raised"])
s.rput(mx+mw-1,top+1,"/",fg=T["dim"],bg=T["raised"])
s.put(mx,top+2,"128 ileti · 3 okunmamış",T["faint"])
s.rput(mx+mw,top+2,"Yeniden eskiye",fg=T["faint"])
s.put(LW+1,top+3,"─"*(MW-2),T["border"])
msgs=[
 (True,"Mehmet Kaya","Akşam yemeği?","14:31",False,True),
 (True,"Turkcell","Kasım 2026 faturanız hazır","13:05",True,False),
 (True,"GitHub","[notify-guard] v0.2 derleme tamam","11:48",False,False),
 (False,"Ayşe Demir","Re: Hafta sonu planı","Dün",False,False),
 (False,"Trendyol","Siparişiniz kargoya verildi","Dün",False,False),
 (False,"Zeynep Arslan","Fotoğraflar","12 Kas",True,False),
 (False,"Google","Güvenlik uyarısı: yeni oturum","12 Kas",False,False),
 (False,"Banka","Ekstre bildirimi","11 Kas",True,False),
 (False,"Spotify","Bu haftanın keşifleri","10 Kas",False,False),
 (False,"Elif Şahin","Re: Doğum günü sürprizi","9 Kas",False,False),
 (False,"Udemy","Kursunuza yeni bölüm eklendi","8 Kas",False,False),
 (False,"Burak Yıldız","Sunum dosyası","7 Kas",True,False),
 (False,"LinkedIn","Profilinizi 12 kişi görüntüledi","7 Kas",False,False),
 (False,"Selin Aydın","Teşekkürler!","6 Kas",False,False),
 (False,"Hepsiburada","Favorinizde indirim var","5 Kas",False,False),
]
selidx=1
y=top+4
for i,(unr,frm,sub,d,att,new) in enumerate(msgs):
    if y>=top+ph-1: break
    bg=(T["sel"] if LF else T["selfocus"]) if i==selidx else None
    if bg: s.fill(LW+1,y,MW-2,1,bg)
    s.put(mx,y,"●" if unr else " ",T["accent"],bg)
    s.put(mx+2,y,frm,T["fg"] if unr else T["dim"],bg,bold=unr,maxw=15)
    s.put(mx+18,y,sub,T["fg"] if unr else T["dim"],bg,bold=unr and i==selidx,maxw=25)
    s.put(mx+44,y,"▪" if att else " ",T["purple"],bg)
    s.rput(mx+mw,y,d,fg=T["dim"] if not unr else T["fg"],bg=bg)
    if new: s.put(mx,y,"●",T["green"],bg)
    y+=1
# reader
rx=LW+MW+2; rw=RW-4
hdr=[("Kimden","Turkcell <fatura@turkcell.com.tr>"),("Kime","ahmet.yilmaz@gmail.com"),("Tarih","14 Kasım 2026, 13:05"),("Konu","Kasım 2026 faturanız hazır")]
y=top+1
for k,v in hdr:
    s.put(rx,y,f"{k:<7}",T["faint"]); s.put(rx+8,y,v,T["fg"],bold=(k=="Konu"),maxw=rw-8); y+=1
s.put(rx,y,"Ek     ",T["faint"]); s.put(rx+8,y,"▪ fatura_kasim_2026.pdf  (212 KB)",T["purple"]); y+=1
s.put(LW+MW+1,y,"─"*(RW-2),T["border"]); y+=2
body=["Sayın Ahmet Yılmaz,","",
"0532 *** ** 47 numaralı hattınıza ait Kasım 2026",
"faturanız oluşturulmuştur.","",
"  Fatura tutarı      :  349,90 TL",
"  Son ödeme tarihi   :  28 Kasım 2026",
"  Fatura dönemi      :  01.11.2026 – 30.11.2026","",
"Faturanızı ekteki dosyadan inceleyebilir, otomatik",
"ödeme talimatı verdiyseniz herhangi bir işlem",
"yapmanıza gerek yoktur.","",
"Bizi tercih ettiğiniz için teşekkür ederiz.","",
"Turkcell Müşteri Hizmetleri"]
for l in body:
    s.put(rx,y,l,T["fg"] if l.strip() else None,maxw=rw); y+=1
s.put(rx,top+ph-2,"Enter: yeni sekmede aç",T["faint"])
if MODE=="list":
    tw,th=44,6; tx=W-tw-2; ty=top+ph-th-3
    s.box(tx,ty,tw,th,fg="#5d7f6a",bg="#26302c")
    s.put(tx+2,ty+1,"● Yeni ileti",T["green"],bold=True)
    s.rput(tx+tw-2,ty+1,"İş · Outlook",fg=T["dim"])
    s.put(tx+2,ty+2,"Ayşe Demir",T["fg"],bold=True)
    s.put(tx+2,ty+3,"Proje toplantısı – yeni saat",T["dim"],maxw=tw-4)
    s.put(tx+2,ty+4,"G: göster",T["faint"]); s.rput(tx+tw-2,ty+4,"4 sn sonra kapanır",fg=T["faint"])
    dy2=ty-4
    s.box(tx,dy2,tw,3,fg=T["border"],bg=T["raised"])
    s.put(tx+2,dy2+1,"Silindi:",T["dim"]); s.put(tx+11,dy2+1,"Kitap önerisi",T["fg"])
    s.put(tx+tw-15,dy2+1," u ",T["fg"],T["key"],bold=True); s.put(tx+tw-11,dy2+1,"geri al",T["accent"])
    s.keybar(H-1,[("↑↓","Gezin"),("Enter","Aç"),("←","Klasörler"),("Tab","Sekmeler"),("n","Yeni"),("r","Yanıtla"),("a","Tümüne"),("f","İlet"),("d","Sil"),("/","Ara")],right="? Yardım")
    print(render(s,"01-ana-ekran","Posta"))
else:
    s.keybar(H-1,[("↑↓","Gezin"),("Enter","Klasöre gir"),("→","Listeye dön"),("Boşluk","Hesabı aç/kapat"),("+","Hesap ekle"),("n","Yeni ileti")],right="? Yardım")
    print(render(s,"02-klasor-paneli","Posta"))
