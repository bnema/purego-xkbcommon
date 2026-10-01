package xkbcommon

import (
	"os"
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
