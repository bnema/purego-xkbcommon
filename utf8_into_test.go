package xkbcommon

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/bnema/purego-xkbcommon/raw"
)

func TestStateUTF8IntoBoundsAndLifetime(t *testing.T) {
	c := live(t)
	s := state(t, rules(t, c, "de"))
	var scratch [MaxUTF8Buffer]byte
	n, err := s.UTF8Into(WaylandKeycode(16), scratch[:])
	if err != nil || !bytes.Equal(scratch[:n], []byte{'q'}) {
		t.Fatalf("ASCII length=%d err=%v", n, err)
	}
	mod, err := rules(t, c, "de").ModIndex("Mod5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateMask(1<<mod, 0, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	n, err = s.UTF8Into(WaylandKeycode(18), scratch[:])
	if err != nil || !bytes.Equal(scratch[:n], []byte{0xe2, 0x82, 0xac}) {
		t.Fatalf("multibyte length=%d err=%v", n, err)
	}
	short := []byte{0xff, 0xff, 0xff}
	n, err = s.UTF8Into(WaylandKeycode(18), short)
	if !errors.Is(err, io.ErrShortBuffer) || n != 3 || !bytes.Equal(short, make([]byte, len(short))) {
		t.Fatalf("short: n=%d err=%v", n, err)
	}
	for _, length := range []int{0, MaxUTF8Buffer + 1} {
		buf := bytes.Repeat([]byte{0xff}, length)
		if _, err := s.UTF8Into(WaylandKeycode(18), buf); !errors.Is(err, ErrUTF8Buffer) {
			t.Fatal(err)
		}
		if !bytes.Equal(buf, make([]byte, length)) {
			t.Fatal("invalid buffer not cleared")
		}
	}
	s.Close()
	for i := range scratch {
		scratch[i] = 0xff
	}
	if _, err := s.UTF8Into(WaylandKeycode(18), scratch[:]); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if !bytes.Equal(scratch[:], make([]byte, len(scratch))) {
		t.Fatal("closed handle left old bytes")
	}
}

func TestComposeUTF8Into(t *testing.T) {
	c := live(t)
	table, err := c.NewComposeTable("en_US.UTF-8")
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	compose, err := table.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer compose.Close()
	for _, sym := range []uint32{raw.XKB_KEY_dead_circumflex, raw.XKB_KEY_e} {
		if _, err := compose.Feed(sym); err != nil {
			t.Fatal(err)
		}
	}
	var buf [MaxUTF8Buffer]byte
	if n, err := compose.UTF8Into(buf[:]); err != nil || !bytes.Equal(buf[:n], []byte{0xc3, 0xaa}) {
		t.Fatalf("compose length=%d err=%v", n, err)
	}
	if n, err := compose.UTF8Into(buf[:2]); !errors.Is(err, io.ErrShortBuffer) || n != 2 {
		t.Fatalf("compose short length=%d err=%v", n, err)
	}
	if buf[0] != 0 || buf[1] != 0 {
		t.Fatal("short compose retained bytes")
	}
	if err := compose.Reset(); err != nil {
		t.Fatal(err)
	}
	if n, err := compose.UTF8Into(buf[:]); err != nil || n != 0 {
		t.Fatalf("reset length=%d err=%v", n, err)
	}
}

func TestUTF8IntoBoundedAllocations(t *testing.T) {
	s := state(t, rules(t, live(t), "us"))
	var buf [MaxUTF8Buffer]byte
	if n, err := s.UTF8Into(WaylandKeycode(16), buf[:]); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := s.UTF8Into(WaylandKeycode(16), buf[:]); err != nil {
			t.Fatal(err)
		}
		clear(buf[:])
	})
	// Purego's native dispatch may allocate fixed ABI scratch; the wrapper
	// must not allocate result-sized buffers or string payloads.
	// Current purego dispatch plus scoped pinning costs six fixed allocations.
	if allocs > 6 {
		t.Fatalf("UTF8Into allocations=%v", allocs)
	}
}

func TestUTF8IntoNilAndClosedReceiversClearScratch(t *testing.T) {
	var nilState *State
	var nilCompose *ComposeState
	buf := []byte{0xff, 0xff, 0xff}
	if n, err := nilState.UTF8Into(WaylandKeycode(16), buf); n != 0 || !errors.Is(err, ErrClosed) {
		t.Fatal(n, err)
	}
	if !bytes.Equal(buf, []byte{0, 0, 0}) {
		t.Fatal("nil state retained bytes")
	}
	for i := range buf {
		buf[i] = 0xff
	}
	if n, err := nilCompose.UTF8Into(buf); n != 0 || !errors.Is(err, ErrClosed) {
		t.Fatal(n, err)
	}
	if !bytes.Equal(buf, []byte{0, 0, 0}) {
		t.Fatal("nil compose retained bytes")
	}
	c := live(t)
	table, err := c.NewComposeTable("en_US.UTF-8")
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	compose, err := table.NewState()
	if err != nil {
		t.Fatal(err)
	}
	compose.Close()
	for i := range buf {
		buf[i] = 0xff
	}
	if n, err := compose.UTF8Into(buf); n != 0 || !errors.Is(err, ErrClosed) {
		t.Fatal(n, err)
	}
	if !bytes.Equal(buf, []byte{0, 0, 0}) {
		t.Fatal("closed compose retained bytes")
	}
}
