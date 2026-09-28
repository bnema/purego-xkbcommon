package xkbcommon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
	short := keymapFile(t, []byte("short"))
	if _, e := c.NewKeymapFD(int(short.Fd()), 4096); e == nil || !strings.Contains(e.Error(), "short read") {
		t.Fatalf("truncated keymap: %v", e)
	}
	if _, e := short.Stat(); e != nil {
		t.Fatalf("FD was closed on error: %v", e)
	}
	for _, size := range []int{0, 16<<20 + 1} {
		if _, e := c.NewKeymapFD(int(short.Fd()), size); e == nil {
			t.Fatalf("accepted size %d", size)
		}
	}
	if _, e := c.NewKeymapFD(-1, 5); e == nil {
		t.Fatal("negative FD")
	}
	if _, e := exec.LookPath("xkbcli"); e != nil {
		t.Skipf("xkbcli compile-keymap reference unavailable: %v", e)
	}
	out, e := exec.Command("xkbcli", "compile-keymap", "--rules", "evdev", "--model", "pc105", "--layout", "fr", "--output-format", "1").Output()
	if e != nil {
		t.Fatalf("reference xkbcli: %v", e)
	}
	f := keymapFile(t, append(out, 0))
	other, e := c.NewKeymapFD(int(f.Fd()), len(out)+1)
	if e != nil {
		t.Fatal(e)
	}
	other.Close()
	if _, e = f.Stat(); e != nil {
		t.Fatalf("FD was closed: %v", e)
	}
	if _, e = c.NewKeymapFD(int(f.Fd()), len(out)+2); e == nil || !strings.Contains(e.Error(), "short read") {
		t.Fatalf("truncated keymap: %v", e)
	}
	if _, e = c.NewKeymapFD(int(f.Fd()), len(out)); e != nil {
		t.Fatalf("non-NUL text: %v", e)
	}
}

func keymapFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	f, e := os.CreateTemp(t.TempDir(), "keymap")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.Close() })
	if _, e = f.Write(data); e != nil {
		t.Fatal(e)
	}
	return f
}

func TestLoaderDiagnostics(t *testing.T) {
	_, e := resolveLibrary("/no/such/libxkbcommon.so", raw.Symbols)
	if e == nil || !strings.Contains(e.Error(), "load /no/such/libxkbcommon.so") {
		t.Fatalf("missing library: %v", e)
	}
	if e := Available(); e != nil {
		if strings.Contains(e.Error(), "load libxkbcommon.so.0") {
			t.Skipf("missing symbol test needs libxkbcommon.so.0: %v", e)
		}
		t.Fatal(e)
	}
	_, e = resolveLibrary("libxkbcommon.so.0", []string{"xkb_nonexistent_test_symbol"})
	if e == nil || !strings.Contains(e.Error(), "missing xkb_nonexistent_test_symbol") {
		t.Fatalf("missing symbol: %v", e)
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

// These expectations are fixed evdev/XKB facts, not values computed by the
// binding under test. Every case compiles through the Wayland FD path.
func TestFDReferenceKeyEvents(t *testing.T) {
	c := live(t)
	for _, tc := range []struct {
		layout, key, utf8, modifiers string
		evdev, group, sym            uint32
	}{
		{"us", "q", "q", "", 16, 0, raw.XKB_KEY_q},
		{"us", "Q", "Q", "Shift", 16, 0, raw.XKB_KEY_Q},
		{"us", "Q", "Q", "Lock", 16, 0, raw.XKB_KEY_Q},
		{"fr", "a", "a", "", 16, 0, raw.XKB_KEY_a},
		{"fr", "€", "€", "Mod5", 18, 0, raw.XKB_KEY_EuroSign},
		{"de", "z", "z", "", 21, 0, raw.XKB_KEY_z},
		{"de", "@", "@", "Mod5", 16, 0, raw.XKB_KEY_at},
		{"de", "dead_circumflex", "", "", 41, 0, raw.XKB_KEY_dead_circumflex},
		{"us,fr", "a", "a", "", 16, 1, raw.XKB_KEY_a},
	} {
		t.Run(tc.layout+"/"+tc.key+"/"+tc.modifiers, func(t *testing.T) {
			// Wayland-format keymaps pinned in testdata/keymaps were produced by
			// `xkbcli compile-keymap --output-format 1` (libxkbcommon 1.13.2), so
			// the FD path is always exercised without optional tools.
			text, e := os.ReadFile("testdata/keymaps/" + strings.ReplaceAll(tc.layout, ",", "_") + ".xkb")
			if e != nil {
				t.Fatal(e)
			}
			f := keymapFile(t, append(text, 0))
			k, e := c.NewKeymapFD(int(f.Fd()), len(text)+1)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { k.Close() })
			s := state(t, k)
			var mask uint32
			if tc.modifiers != "" {
				idx, e := k.ModIndex(tc.modifiers)
				if e != nil || idx >= 32 {
					t.Fatalf("modifier %s: %d %v", tc.modifiers, idx, e)
				}
				mask = 1 << idx
			}
			var depressed, locked uint32
			if tc.modifiers == "Lock" {
				locked = mask
			} else {
				depressed = mask
			}
			if _, e := s.UpdateMask(depressed, 0, locked, 0, 0, tc.group); e != nil {
				t.Fatal(e)
			}
			if group, e := s.Layout(); e != nil || group != tc.group {
				t.Fatalf("group %d: %v", group, e)
			}
			code := WaylandKeycode(tc.evdev)
			if sym, e := s.KeySym(code); e != nil || sym != tc.sym {
				t.Fatalf("keysym %#x want %#x: %v", sym, tc.sym, e)
			}
			if v, e := s.UTF8(code); e != nil || v != tc.utf8 {
				t.Fatalf("utf8 %q want %q: %v", v, tc.utf8, e)
			}
			if tc.sym == raw.XKB_KEY_dead_circumflex {
				if v, e := s.UTF8(WaylandKeycode(18)); e != nil || v != "e" {
					t.Fatalf("compose input e: %q %v", v, e)
				}
				table, e := c.NewComposeTable("en_US.UTF-8")
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { table.Close() })
				compose, e := table.NewState()
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { compose.Close() })
				if _, e := compose.Feed(tc.sym); e != nil {
					t.Fatal(e)
				}
				if _, e := compose.Feed(raw.XKB_KEY_e); e != nil {
					t.Fatal(e)
				}
				if status, e := compose.Status(); e != nil || status != raw.XKB_COMPOSE_COMPOSED {
					t.Fatalf("compose status %d: %v", status, e)
				}
				if sym, e := compose.KeySym(); e != nil || sym != raw.XKB_KEY_ecircumflex {
					t.Fatalf("compose sym %#x: %v", sym, e)
				}
				if v, e := compose.UTF8(); e != nil || v != "ê" {
					t.Fatalf("compose utf8 %q: %v", v, e)
				}
			}
			if _, e := exec.LookPath("xkbcli"); e == nil && tc.group == 0 && tc.modifiers != "Lock" {
				args := []string{"how-to-type", "--layout", tc.layout}
				if tc.sym == raw.XKB_KEY_dead_circumflex {
					args = append(args, "--keysym")
				}
				out, e := exec.Command("xkbcli", append(args, tc.key)...).Output()
				if e != nil {
					t.Fatal(e)
				}
				// The CLI reports XKB keycodes, not evdev codes.
				pattern := regexp.MustCompile(`(?m)^\s*` + fmt.Sprint(code) + `\s+\S+\s+\d+\s+.*\[.*\]`)
				if !pattern.Match(out) {
					t.Fatalf("xkbcli how-to-type lacks keycode %d: %s", code, out)
				}
			}
		})
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
