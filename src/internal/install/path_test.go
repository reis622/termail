package install

import "testing"

func TestMergePath(t *testing.T) {
	dir := `C:\Users\Ahmet\AppData\Local\Programs\Termail`
	cases := []struct {
		in, want string
		changed  bool
	}{
		{"", dir, true},
		{`C:\Windows;%USERPROFILE%\bin`, `C:\Windows;%USERPROFILE%\bin;` + dir, true},
		{`C:\Windows;;C:\Tools;`, `C:\Windows;C:\Tools;` + dir, true},
		{`C:\Windows;` + dir, `C:\Windows;` + dir, false},
		{`c:\users\ahmet\appdata\local\programs\termail\;C:\Go\bin`, `c:\users\ahmet\appdata\local\programs\termail\;C:\Go\bin`, false},
		{`"` + dir + `"`, `"` + dir + `"`, false},
	}
	for _, c := range cases {
		got, ch := MergePath(c.in, dir)
		if got != c.want || ch != c.changed {
			t.Errorf("MergePath(%q) = %q,%v want %q,%v", c.in, got, ch, c.want, c.changed)
		}
		// merging twice never duplicates
		again, ch2 := MergePath(got, dir)
		if again != got || ch2 {
			t.Errorf("second merge changed %q -> %q", got, again)
		}
	}
}

func TestRemovePath(t *testing.T) {
	dir := `C:\Users\Ahmet\AppData\Local\Programs\Termail`
	got, ch := RemovePath(`C:\Windows;`+dir+`;%USERPROFILE%\bin;C:\USERS\AHMET\APPDATA\LOCAL\PROGRAMS\TERMAIL\`, dir)
	if got != `C:\Windows;%USERPROFILE%\bin` || !ch {
		t.Fatalf("%q %v", got, ch)
	}
	got, ch = RemovePath(`C:\Windows`, dir)
	if got != `C:\Windows` || ch {
		t.Fatalf("%q %v", got, ch)
	}
}
