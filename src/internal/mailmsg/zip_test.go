package mailmsg

import (
	"archive/zip"
	"io"
)

func newZip(w io.Writer, name string) *zip.Writer {
	zw := zip.NewWriter(w)
	f, _ := zw.Create(name)
	f.Write([]byte("MZ"))
	return zw
}
