package xkbcommon

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unsafe"

	"github.com/bnema/purego-xkbcommon/raw"
)

func live(t *testing.T) *Context {
	t.Helper()
	if err := Available(); err != nil {
		if strings.Contains(err.Error(), "load libxkbcommon.so.0") {
			t.Skipf("live libxkbcommon.so.0 absent: %v", err)
		}
		t.Fatal(err)
	}
	c, e := NewContext()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func rules(t *testing.T, c *Context, layout string) *Keymap {
	t.Helper()
	k, e := c.NewKeymapRules("evdev", "pc105", layout, "", "")
	if e != nil {
		t.Fatalf("rules %s: %v", layout, e)
	}
	t.Cleanup(func() { k.Close() })
	return k
}
func state(t *testing.T, k *Keymap) *State {
	t.Helper()
	s, e := k.NewState()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestABI(t *testing.T) {
	if unsafe.Sizeof(raw.RuleNames{}) != 5*unsafe.Sizeof(uintptr(0)) {
		t.Fatal("rule names ABI")
	}
	if unsafe.Sizeof(raw.Keycode(0)) != 4 || unsafe.Sizeof(raw.Keysym(0)) != 4 || unsafe.Sizeof(raw.ModMask(0)) != 4 {
		t.Fatal("scalar ABI")
	}
}
func TestSymbols(t *testing.T) {
	live(t)
	if len(raw.Symbols) < 25 {
		t.Fatal("incomplete required symbol set")
	}
}
func TestMalformedAndLifetime(t *testing.T) {
	c := live(t)
	if _, e := c.NewKeymapString("garbage"); e == nil {
		t.Fatal("malformed keymap compiled")
	}
	if _, e := c.NewKeymapString("foo\x00bar"); e == nil {
		t.Fatal("embedded NUL")
	}
	k := rules(t, c, "us")
	s := state(t, k)
	c.Close()
	k.Close()
	for i := 0; i < 3; i++ {
		c.Close()
		k.Close()
	}
	if _, e := k.NewState(); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
	v, e := s.UTF8(38)
	if e != nil || v != "a" {
		t.Fatalf("state after parent close: %q %v", v, e)
	}
	s.Close()
	s.Close()
	if _, e := s.KeySym(38); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
}
func TestFD(t *testing.T) {
	c := live(t)
	out, e := exec.Command("xkbcli", "compile-keymap", "--rules", "evdev", "--model", "pc105", "--layout", "fr", "--output-format", "1").Output()
	if e != nil {
		t.Fatalf("reference xkbcli: %v", e)
	}
	f, e := os.CreateTemp(t.TempDir(), "keymap")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	data := append(out, 0)
	if _, e = f.Write(data); e != nil {
		t.Fatal(e)
	}
	other, e := c.NewKeymapFD(int(f.Fd()), len(data))
	if e != nil {
		t.Fatal(e)
	}
	other.Close()
	if _, e = f.Stat(); e != nil {
		t.Fatalf("FD was closed: %v", e)
	}
	if _, e = c.NewKeymapFD(int(f.Fd()), len(data)-1); e == nil {
		t.Fatal("missing NUL accepted")
	}
	if _, e = c.NewKeymapFD(-1, len(data)); e == nil {
		t.Fatal("negative FD")
	}
}
func TestLayoutsAndDifferential(t *testing.T) {
	c := live(t)
	for _, tc := range []struct {
		layout string
		code   uint32
		want   string
		alt    bool
	}{{"us", 38, "a", false}, {"fr", 24, "a", false}, {"de", 29, "z", false}, {"de", 26, "€", true}, {"fr", 26, "€", true}} {
		t.Run(tc.layout+tc.want, func(t *testing.T) {
			k := rules(t, c, tc.layout)
			s := state(t, k)
			if tc.alt {
				idx, e := k.ModIndex("LevelThree")
				if e != nil || idx == ^uint32(0) || idx >= 32 {
					t.Fatalf("LevelThree: %d %v", idx, e)
				}
				s.UpdateMask(1<<idx, 0, 0, 0, 0, 0)
			}
			got, e := s.UTF8(tc.code)
			if e != nil || got != tc.want {
				t.Fatalf("utf8 %q want %q: %v", got, tc.want, e)
			}
			sym, e := s.KeySym(tc.code)
			if e != nil {
				t.Fatal(e)
			}
			name, e := KeysymName(sym)
			if e != nil || name == "" {
				t.Fatalf("name %q: %v", name, e)
			}
			back, e := KeysymFromName(name)
			if e != nil || back != sym {
				t.Fatalf("name roundtrip %d %d %v", sym, back, e)
			}
			// Compare safe calls to the independently resolved native entry points for this real rules keymap.
			nativeSym := loader.f.Xkb_state_key_get_one_sym(s.h, tc.code)
			if nativeSym != sym {
				t.Fatalf("native sym %d != %d", nativeSym, sym)
			}
			nativeN := loader.f.Xkb_state_key_get_utf8(s.h, tc.code, 0, 0)
			if nativeN != int32(len(got)) {
				t.Fatalf("native bytes %d != %d", nativeN, len(got))
			}
		})
	}
	k := rules(t, c, "us,de")
	s := state(t, k)
	n, e := k.LayoutCount()
	if e != nil || n != 2 {
		t.Fatalf("layouts %d %v", n, e)
	}
	s.UpdateMask(0, 0, 0, 0, 0, 1)
	group, e := s.Layout()
	if e != nil || group != 1 {
		t.Fatalf("group %d %v", group, e)
	}
	v, _ := s.UTF8(29)
	if v != "z" {
		t.Fatalf("group 1 %q", v)
	}
	shift, e := k.ModIndex("Shift")
	if e != nil || shift >= 32 {
		t.Fatalf("shift %d %v", shift, e)
	}
	s.UpdateMask(1<<shift, 0, 0, 0, 0, 1)
	v, _ = s.UTF8(29)
	if v != "Z" {
		t.Fatalf("shift group 1 %q", v)
	}
	mods, _ := s.Mods()
	if mods&(1<<shift) == 0 {
		t.Fatalf("mods %#x", mods)
	}
}
func TestCompose(t *testing.T) {
	c := live(t)
	table, e := c.NewComposeTable("en_US.UTF-8")
	if e != nil {
		t.Fatal(e)
	}
	s, e := table.NewState()
	if e != nil {
		t.Fatal(e)
	}
	table.Close()
	c.Close()
	if _, e = s.Feed(raw.XKB_KEY_dead_acute); e != nil {
		t.Fatal(e)
	}
	status, _ := s.Status()
	if status != raw.XKB_COMPOSE_COMPOSING {
		t.Fatalf("status %d", status)
	}
	s.Feed(raw.XKB_KEY_e)
	status, _ = s.Status()
	if status != raw.XKB_COMPOSE_COMPOSED {
		t.Fatalf("status %d", status)
	}
	v, e := s.UTF8()
	if e != nil || v != "é" {
		t.Fatalf("compose %q %v", v, e)
	}
	s.Reset()
	s.Feed(raw.XKB_KEY_Multi_key)
	s.Feed(raw.XKB_KEY_apostrophe)
	s.Feed(raw.XKB_KEY_e)
	v, _ = s.UTF8()
	if v != "é" {
		t.Fatalf("multikey %q", v)
	}
	s.Close()
	s.Close()
	table.Close()
	if _, e = s.UTF8(); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
}
func TestSizing(t *testing.T) {
	v, e := sizedUTF8(func(p, n uintptr) int32 { return 0 })
	if e != nil || v != "" {
		t.Fatalf("%q %v", v, e)
	}
	_, e = sizedUTF8(func(p, n uintptr) int32 { return -1 })
	if e == nil {
		t.Fatal("negative result accepted")
	}
	v, e = KeysymName(raw.XKB_KEY_EuroSign)
	if e != nil || v != "EuroSign" {
		t.Fatalf("name %q %v", v, e)
	}
}
