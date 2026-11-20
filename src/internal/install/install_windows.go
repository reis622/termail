//go:build windows

package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func targetDir() (string, error) {
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		return "", errors.New("kullanıcı klasörü bulunamadı")
	}
	return Dir(la), nil
}

// Install copies the running program to %LOCALAPPDATA%\Programs\Termail and
// adds that folder to the user's PATH.
func Install(out io.Writer) error {
	dir, err := targetDir()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, "termail.exe")
	if !samePath(self, dst) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := copyFile(self, dst); err != nil {
			return fmt.Errorf("program kopyalanamadı (açık bir Termail penceresi varsa kapatıp tekrar dene): %w", err)
		}
	}
	changed, err := editUserPath(func(p string) (string, bool) { return MergePath(p, dir) })
	if err != nil {
		return fmt.Errorf("arama yoluna eklenemedi: %w", err)
	}
	if changed {
		broadcastEnvChange()
	}
	fmt.Fprintf(out, "Termail şuraya kopyalandı: %s\n", dst)
	fmt.Fprintln(out, "Kuruldu. Yeni bir terminal açıp termail yaz.")
	return nil
}

// Uninstall removes the folder from PATH and deletes the program. Settings
// and saved sign-ins are kept.
func Uninstall(out io.Writer) error {
	dir, err := targetDir()
	if err != nil {
		return err
	}
	changed, err := editUserPath(func(p string) (string, bool) { return RemovePath(p, dir) })
	if err != nil {
		return fmt.Errorf("arama yolundan çıkarılamadı: %w", err)
	}
	if changed {
		broadcastEnvChange()
	}
	self, _ := os.Executable()
	if self != "" && samePath(filepath.Dir(self), dir) {
		// A running program cannot delete itself: let a helper remove the
		// folder a moment after we exit.
		cmd := exec.Command("cmd", "/c", "ping 127.0.0.1 -n 3 >nul & rmdir /s /q \""+dir+"\"")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
		_ = cmd.Start()
	} else if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("program klasörü silinemedi: %w", err)
	}
	fmt.Fprintln(out, "Termail kaldırıldı. Ayarların ve kayıtlı girişlerin yerinde duruyor.")
	return nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".yeni"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, in); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// editUserPath rewrites HKCU\Environment\Path, keeping its value type
// (usually REG_EXPAND_SZ so %VARIABLES% keep working).
func editUserPath(edit func(string) (string, bool)) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	cur, typ, err := k.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}
	next, changed := edit(cur)
	if !changed {
		return false, nil
	}
	if typ == registry.SZ {
		return true, k.SetStringValue("Path", next)
	}
	return true, k.SetExpandStringValue("Path", next)
}

// broadcastEnvChange tells Explorer (and new terminals it starts) that the
// environment changed, so no sign-out is needed.
func broadcastEnvChange() {
	const hwndBroadcast = 0xffff
	const wmSettingChange = 0x001A
	const smtoAbortIfHung = 0x0002
	env, _ := windows.UTF16PtrFromString("Environment")
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var res uintptr
	proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&res)))
}
