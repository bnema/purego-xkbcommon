package xkbcommon

import (
	"bytes"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/bnema/purego-xkbcommon/raw"
)

// fdKeymap compiles a pinned Wayland-format keymap from testdata.
func fdKeymap(t *testing.T, c *Context, layout string) *Keymap {
	t.Helper()
	text, e := os.ReadFile("testdata/keymaps/" + layout + ".xkb")
	if e != nil {
		t.Fatal(e)
	}
	f := keymapFile(t, append(text, 0))
	k, e := c.NewKeymapFD(int(f.Fd()), len(text)+1)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { k.Close() })
	return k
}

func assertNoAllocs(t *testing.T, name string, fn func()) {
	t.Helper()
	fn() // warm up lazy runtime state
	if n := testing.AllocsPerRun(200, fn); n != 0 {
		t.Errorf("%s: %v allocs/op, want 0", name, n)
	} else {
		t.Logf("%s: 0 allocs/op", name)
	}
}

func TestHotPathAllocs(t *testing.T) {
	c := live(t)
	k := fdKeymap(t, c, "fr")
	s := state(t, k)
	code := WaylandKeycode(16)
	var buf [MaxUTF8Buffer]byte
	var fail error
	check := func(e error) {
		if e != nil {
			fail = e
		}
	}

	table, e := c.NewComposeTable("en_US.UTF-8")
	if e != nil {
		t.Fatal(e)
	}
	defer table.Close()
	compose, e := table.NewState()
	if e != nil {
		t.Fatal(e)
	}
	defer compose.Close()

	assertNoAllocs(t, "State.KeySym", func() { _, e := s.KeySym(code); check(e) })
	assertNoAllocs(t, "State.UTF8Into", func() { _, e := s.UTF8Into(code, buf[:]); check(e) })
	assertNoAllocs(t, "State.UpdateMask", func() { _, e := s.UpdateMask(1, 0, 2, 0, 0, 0); check(e) })
	assertNoAllocs(t, "State.Layout", func() { _, e := s.Layout(); check(e) })
	assertNoAllocs(t, "State.Mods", func() { _, e := s.Mods(); check(e) })
	assertNoAllocs(t, "State.ModIndexActive", func() { _, e := s.ModIndexActive(0); check(e) })
	assertNoAllocs(t, "State.ModNameActive", func() { _, e := s.ModNameActive(raw.XKB_MOD_NAME_CTRL); check(e) })
	assertNoAllocs(t, "Keymap.KeyRepeats", func() { _, e := k.KeyRepeats(code); check(e) })
	assertNoAllocs(t, "KeysymNameInto", func() { _, e := KeysymNameInto(raw.XKB_KEY_a, buf[:]); check(e) })
	assertNoAllocs(t, "compose key sequence", func() {
		_, e := compose.Feed(raw.XKB_KEY_dead_circumflex)
		check(e)
		_, e = compose.Feed(raw.XKB_KEY_e)
		check(e)
		_, e = compose.Status()
		check(e)
		_, e = compose.KeySym()
		check(e)
		_, e = compose.UTF8Into(buf[:])
		check(e)
		check(compose.Reset())
	})
	if fail != nil {
		t.Fatal(fail)
	}
}

// TestHotPathValues guards the fixed-arity dispatch against argument/return
// conversion mistakes.
func TestHotPathValues(t *testing.T) {
	c := live(t)
	k := fdKeymap(t, c, "us")
	s := state(t, k)
	if r, e := k.KeyRepeats(WaylandKeycode(30)); e != nil || !r {
		t.Fatalf("KeyRepeats(a) = %v, %v", r, e)
	}
	ctrl, e := k.ModIndex(raw.XKB_MOD_NAME_CTRL)
	if e != nil {
		t.Fatal(e)
	}
	if a, e := s.ModNameActive(raw.XKB_MOD_NAME_CTRL); e != nil || a {
		t.Fatalf("ctrl active before update: %v %v", a, e)
	}
	if _, e := s.UpdateMask(1<<ctrl, 0, 0, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	if a, e := s.ModNameActive(raw.XKB_MOD_NAME_CTRL); e != nil || !a {
		t.Fatalf("ctrl name active: %v %v", a, e)
	}
	if a, e := s.ModIndexActive(ctrl); e != nil || !a {
		t.Fatalf("ctrl index active: %v %v", a, e)
	}
	if a, e := s.ModNameActive("NoSuchModifier"); e != nil || a {
		t.Fatalf("unknown modifier active: %v %v", a, e)
	}
	var buf [16]byte
	n, e := KeysymNameInto(raw.XKB_KEY_EuroSign, buf[:])
	if e != nil || string(buf[:n]) != "EuroSign" {
		t.Fatalf("KeysymNameInto = %q %v", buf[:n], e)
	}
}

func TestModNameActiveLongName(t *testing.T) {
	c := live(t)
	k := fdKeymap(t, c, "us")
	s := state(t, k)
	for _, n := range []int{62, 63, 64, 65, 200} {
		name := strings.Repeat("x", n)
		if a, e := s.ModNameActive(name); e != nil || a {
			t.Fatalf("len %d: %v %v", n, a, e)
		}
	}
	// A short name after a long one must not see stale scratch bytes.
	ctrl, e := k.ModIndex(raw.XKB_MOD_NAME_CTRL)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.UpdateMask(1<<ctrl, 0, 0, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	if a, e := s.ModNameActive(strings.Repeat("y", 63)); e != nil || a {
		t.Fatal(a, e)
	}
	if a, e := s.ModNameActive(raw.XKB_MOD_NAME_CTRL); e != nil || !a {
		t.Fatal(a, e)
	}
	if _, e := s.ModNameActive("a\x00b"); e == nil {
		t.Fatal("embedded NUL accepted")
	}
}

func TestKeysymNameIntoShortBuffer(t *testing.T) {
	live(t)
	buf := []byte{0xff, 0xff, 0xff}
	n, e := KeysymNameInto(raw.XKB_KEY_EuroSign, buf)
	if !errors.Is(e, io.ErrShortBuffer) || n != len("EuroSign") || !bytes.Equal(buf, make([]byte, 3)) {
		t.Fatalf("n=%d err=%v buf=%v", n, e, buf)
	}
	if _, e = KeysymNameInto(raw.XKB_KEY_a, nil); !errors.Is(e, ErrUTF8Buffer) {
		t.Fatal(e)
	}
}

func TestModIndexActiveInvalid(t *testing.T) {
	c := live(t)
	s := state(t, fdKeymap(t, c, "us"))
	for _, idx := range []uint32{64, 1 << 20, ^uint32(0)} {
		if a, e := s.ModIndexActive(idx); e != nil || a {
			t.Fatalf("idx %d: %v %v", idx, a, e)
		}
	}
	s.Close()
	if _, e := s.ModIndexActive(0); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
	if _, e := s.ModNameActive("Shift"); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
}

func TestNewKeymapRulesNames(t *testing.T) {
	c := live(t)
	// Every RMLVO component is exercised so each pointer in RuleNames must stay valid.
	k, e := c.NewKeymapRules("evdev", "pc105", "us,fr", "dvorak,", "grp:alt_shift_toggle")
	if e != nil {
		t.Fatal(e)
	}
	defer k.Close()
	if n, e := k.LayoutCount(); e != nil || n != 2 {
		t.Fatalf("layouts %d %v", n, e)
	}
	s := state(t, k)
	runtime.GC()
	// Dvorak puts apostrophe on the evdev KEY_Q position.
	if sym, e := s.KeySym(WaylandKeycode(16)); e != nil || sym != raw.XKB_KEY_apostrophe {
		t.Fatalf("dvorak sym %#x %v", sym, e)
	}
	// Defaults (all empty) still work, and an unknown layout is rejected.
	d, e := c.NewKeymapRules("", "", "", "", "")
	if e != nil {
		t.Fatal(e)
	}
	d.Close()
	if _, e = c.NewKeymapRules("evdev", "pc105", "nosuchlayout", "", ""); e == nil {
		t.Fatal("unknown layout accepted")
	}
}

func TestZeroFuncsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("zero raw.Funcs must panic, not call address 0")
		}
	}()
	var f raw.Funcs
	f.Xkb_keymap_num_layouts(0)
}
