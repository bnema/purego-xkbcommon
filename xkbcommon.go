// Package xkbcommon interprets Wayland keyboard maps with libxkbcommon,
// loaded at runtime without cgo. Close owned values when they are no longer used.
package xkbcommon

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/bnema/purego-xkbcommon/raw"
)

var ErrClosed = errors.New("xkbcommon: closed handle")

func cstr(s string) []byte { return append([]byte(s), 0) }

// ptr returns the address of b's first byte, or nil. The result is only valid
// as an argument to a raw.Funcs method, whose uintptr conversion keeps the
// backing array alive and (via //go:uintptrescapes) on the heap for the call.
func ptr(b []byte) unsafe.Pointer {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Pointer(&b[0])
}
func validString(s string) error {
	if strings.IndexByte(s, 0) >= 0 {
		return errors.New("xkbcommon: embedded NUL")
	}
	return nil
}

// Keycodes from wl_keyboard are evdev codes; add 8 before calling State.KeySym or State.UTF8.
func WaylandKeycode(evdev uint32) uint32 { return evdev + 8 }

type Context struct {
	mu sync.Mutex
	h  uintptr
}

func NewContext() (*Context, error) {
	if err := Available(); err != nil {
		return nil, err
	}
	h := loader.f.Xkb_context_new(0)
	if h == 0 {
		return nil, errors.New("xkbcommon: create context failed")
	}
	return &Context{h: h}, nil
}
func (c *Context) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.h != 0 {
		loader.f.Xkb_context_unref(c.h)
		c.h = 0
	}
	return nil
}
func (c *Context) retain() (uintptr, error) {
	if c == nil {
		return 0, ErrClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.h == 0 {
		return 0, ErrClosed
	}
	loader.f.Xkb_context_ref(c.h)
	return c.h, nil
}
func releaseContext(h uintptr) { loader.f.Xkb_context_unref(h) }

type Keymap struct {
	mu         sync.Mutex
	h, context uintptr
}

func newKeymap(ctx *Context, build func(uintptr) uintptr) (*Keymap, error) {
	h, e := ctx.retain()
	if e != nil {
		return nil, e
	}
	k := build(h)
	if k == 0 {
		releaseContext(h)
		return nil, errors.New("xkbcommon: invalid keymap")
	}
	return &Keymap{h: k, context: h}, nil
}

// NewKeymapString compiles a NUL-free xkb_v1 text keymap.
func (c *Context) NewKeymapString(text string) (*Keymap, error) {
	if e := validString(text); e != nil {
		return nil, e
	}
	b := cstr(text)
	return newKeymap(c, func(h uintptr) uintptr {
		v := loader.f.Xkb_keymap_new_from_string(h, ptr(b), raw.XKB_KEYMAP_FORMAT_TEXT_V1, 0)
		runtime.KeepAlive(b)
		return v
	})
}

// NewKeymapRules compiles an evdev rules keymap. Empty rules/model/variant/options
// use libxkbcommon defaults; layout is e.g. "us", "fr", "de", or "us,de".
func (c *Context) NewKeymapRules(rules, model, layout, variant, options string) (*Keymap, error) {
	args := []string{rules, model, layout, variant, options}
	var b [5][]byte
	var n raw.RuleNames
	p := []*unsafe.Pointer{&n.Rules, &n.Model, &n.Layout, &n.Variant, &n.Options}
	for i, s := range args {
		if e := validString(s); e != nil {
			return nil, e
		}
		if s != "" {
			b[i] = cstr(s)
			*p[i] = ptr(b[i])
		}
	}
	return newKeymap(c, func(h uintptr) uintptr {
		v := loader.f.Xkb_keymap_new_from_names(h, unsafe.Pointer(&n), 0)
		runtime.KeepAlive(b)
		runtime.KeepAlive(n)
		return v
	})
}

// NewKeymapFD copies exactly size bytes from offset zero of a Wayland xkb_v1
// keymap FD (at most 16 MiB). A trailing Wayland NUL, if present, is excluded
// from the compiled text. The caller retains ownership of fd, even on error.
func (c *Context) NewKeymapFD(fd int, size int) (*Keymap, error) {
	if fd < 0 || size < 1 || size > 16<<20 {
		return nil, errors.New("xkbcommon: invalid keymap FD or size (limit 16 MiB)")
	}
	var stat syscall.Stat_t
	if e := syscall.Fstat(fd, &stat); e != nil {
		return nil, fmt.Errorf("xkbcommon: stat keymap FD: %w", e)
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, errors.New("xkbcommon: keymap FD is not a regular file")
	}
	b := make([]byte, size)
	for offset := 0; offset < size; {
		n, e := syscall.Pread(fd, b[offset:], int64(offset))
		offset += n
		if e == syscall.EINTR {
			continue
		}
		if e != nil {
			return nil, fmt.Errorf("xkbcommon: read keymap FD: %w", e)
		}
		if n == 0 {
			return nil, fmt.Errorf("xkbcommon: read keymap FD: %w", errors.New("short read"))
		}
	}
	if b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return newKeymap(c, func(h uintptr) uintptr {
		v := loader.f.Xkb_keymap_new_from_buffer(h, ptr(b), uintptr(len(b)), raw.XKB_KEYMAP_FORMAT_TEXT_V1, 0)
		runtime.KeepAlive(b)
		return v
	})
}
func (k *Keymap) Close() error {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	h, ctx := k.h, k.context
	k.h = 0
	k.context = 0
	if h != 0 {
		loader.f.Xkb_keymap_unref(h)
	}
	k.mu.Unlock()
	if ctx != 0 {
		releaseContext(ctx)
	}
	return nil
}
func (k *Keymap) retain() (uintptr, error) {
	if k == nil {
		return 0, ErrClosed
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.h == 0 {
		return 0, ErrClosed
	}
	loader.f.Xkb_keymap_ref(k.h)
	return k.h, nil
}
func (k *Keymap) ModIndex(name string) (uint32, error) {
	if e := validString(name); e != nil {
		return 0, e
	}
	if k == nil {
		return 0, ErrClosed
	}
	b := cstr(name)
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.h == 0 {
		return 0, ErrClosed
	}
	v := loader.f.Xkb_keymap_mod_get_index(k.h, ptr(b))
	runtime.KeepAlive(b)
	return v, nil
}

// KeyRepeats reports whether the key repeats when held. It does not allocate.
func (k *Keymap) KeyRepeats(keycode uint32) (bool, error) {
	if k == nil {
		return false, ErrClosed
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.h == 0 {
		return false, ErrClosed
	}
	return loader.f.Xkb_keymap_key_repeats(k.h, keycode) > 0, nil
}
func (k *Keymap) LayoutCount() (uint32, error) {
	if k == nil {
		return 0, ErrClosed
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.h == 0 {
		return 0, ErrClosed
	}
	return loader.f.Xkb_keymap_num_layouts(k.h), nil
}
func (k *Keymap) NewState() (*State, error) {
	h, e := k.retain()
	if e != nil {
		return nil, e
	}
	s := loader.f.Xkb_state_new(h)
	if s == 0 {
		loader.f.Xkb_keymap_unref(h)
		return nil, errors.New("xkbcommon: create state failed")
	}
	return &State{h: s, keymap: h}, nil
}

type State struct {
	mu        sync.Mutex
	h, keymap uintptr
	name      [64]byte // NUL-terminated modifier-name scratch, guarded by mu
}

func (s *State) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	h, k := s.h, s.keymap
	s.h = 0
	s.keymap = 0
	if h != 0 {
		loader.f.Xkb_state_unref(h)
	}
	s.mu.Unlock()
	if k != 0 {
		loader.f.Xkb_keymap_unref(k)
	}
	return nil
}

// UpdateMask accepts depressed/latched/locked modifier masks and layout group indices
// in the order supplied by wl_keyboard.modifiers. It returns changed components.
func (s *State) UpdateMask(depressed, latched, locked, groupDepressed, groupLatched, groupLocked uint32) (uint32, error) {
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return loader.f.Xkb_state_update_mask(s.h, depressed, latched, locked, groupDepressed, groupLatched, groupLocked), nil
}
func (s *State) Layout() (uint32, error) {
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return loader.f.Xkb_state_serialize_layout(s.h, raw.XKB_STATE_LAYOUT_EFFECTIVE), nil
}

// ModIndexActive reports whether the modifier at idx is effectively active.
// It does not allocate. An unknown index reports false.
func (s *State) ModIndexActive(idx uint32) (bool, error) {
	if s == nil {
		return false, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return false, ErrClosed
	}
	return loader.f.Xkb_state_mod_index_is_active(s.h, idx, raw.XKB_STATE_MODS_EFFECTIVE) > 0, nil
}

// ModNameActive reports whether the named modifier (for example
// raw.XKB_MOD_NAME_CTRL) is effectively active. Names up to 63 bytes do not
// allocate. An unknown name reports false.
func (s *State) ModNameActive(name string) (bool, error) {
	if e := validString(name); e != nil {
		return false, e
	}
	if s == nil {
		return false, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return false, ErrClosed
	}
	var b []byte
	if len(name) < len(s.name) {
		// Heap-resident scratch owned by State and guarded by s.mu.
		b = s.name[:len(name)+1]
		copy(b, name)
		b[len(name)] = 0
	} else {
		b = cstr(name)
	}
	v := loader.f.Xkb_state_mod_name_is_active(s.h, ptr(b), raw.XKB_STATE_MODS_EFFECTIVE)
	runtime.KeepAlive(b)
	return v > 0, nil
}

func (s *State) Mods() (uint32, error) {
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return loader.f.Xkb_state_serialize_mods(s.h, raw.XKB_STATE_MODS_EFFECTIVE), nil
}
func (s *State) KeySym(keycode uint32) (uint32, error) {
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return loader.f.Xkb_state_key_get_one_sym(s.h, keycode), nil
}

// sizedUTF8 probes the required byte count (excluding NUL) and retries if it grows.
func sizedUTF8(call func(unsafe.Pointer, uintptr) int32) (string, error) {
	n := call(nil, 0)
	if n < 0 {
		return "", errors.New("xkbcommon: UTF-8 conversion failed")
	}
	if n == 0 {
		return "", nil
	}
	for attempt := 0; attempt < 3; attempt++ {
		if n > 1<<20 {
			return "", errors.New("xkbcommon: UTF-8 result exceeds 1 MiB")
		}
		b := make([]byte, int(n)+1)
		got := call(ptr(b), uintptr(len(b)))
		runtime.KeepAlive(b)
		if got < 0 {
			return "", errors.New("xkbcommon: UTF-8 conversion failed")
		}
		if int(got) < len(b) {
			return string(b[:got]), nil
		}
		n = got
	}
	return "", errors.New("xkbcommon: UTF-8 result changed during sizing")
}

// MaxUTF8Buffer is the byte API's maximum scratch length, including the
// native trailing NUL. Thus a full result can contain at most 511 payload bytes.
const MaxUTF8Buffer = 512

// ErrUTF8Buffer reports a destination buffer whose length is outside
// 1..MaxUTF8Buffer.
var ErrUTF8Buffer = fmt.Errorf("xkbcommon: UTF-8 buffer length must be 1..%d", MaxUTF8Buffer)

// utf8Into never creates a string or keeps the caller's storage. Native writes
// are synchronous; the Syscall uintptr conversion keeps dst's backing array
// alive and unmoved for the duration of the call (go:uintptrescapes).
// Short buffers are wiped rather than exposing truncated UTF-8 or credentials.
// Callers clear dst before any early return.
func utf8Into(dst []byte, call func(unsafe.Pointer, uintptr) int32) (int, error) {
	if len(dst) < 1 || len(dst) > MaxUTF8Buffer {
		return 0, ErrUTF8Buffer
	}
	n := call(ptr(dst), uintptr(len(dst)))
	runtime.KeepAlive(dst)
	if n < 0 {
		clear(dst)
		return 0, errors.New("xkbcommon: UTF-8 conversion failed")
	}
	if int(n) >= len(dst) {
		clear(dst)
		return int(n), io.ErrShortBuffer
	}
	clear(dst[n:])
	return int(n), nil
}

// UTF8Into writes into caller-owned mutable storage without constructing a
// string. dst is passed to C through a uintptr-escaping call, so its backing
// array lives on the heap (a stack array sliced into dst is moved there once). n excludes NUL; on io.ErrShortBuffer n is the required payload size,
// all of dst is cleared, and the caller needs n+1 bytes. dst must have length
// 1..MaxUTF8Buffer. No storage is retained; the caller owns wiping after use.
func (s *State) UTF8Into(keycode uint32, dst []byte) (int, error) {
	clear(dst)
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return utf8Into(dst, func(p unsafe.Pointer, n uintptr) int32 { return loader.f.Xkb_state_key_get_utf8(s.h, keycode, p, n) })
}

func (s *State) UTF8(keycode uint32) (string, error) {
	if s == nil {
		return "", ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return "", ErrClosed
	}
	return sizedUTF8(func(p unsafe.Pointer, n uintptr) int32 { return loader.f.Xkb_state_key_get_utf8(s.h, keycode, p, n) })
}
func KeysymName(sym uint32) (string, error) {
	if e := Available(); e != nil {
		return "", e
	}
	return sizedUTF8(func(p unsafe.Pointer, n uintptr) int32 { return loader.f.Xkb_keysym_get_name(sym, p, n) })
}

// KeysymNameInto writes the keysym name into dst without allocating, with the
// same bounds and short-buffer semantics as State.UTF8Into.
func KeysymNameInto(sym uint32, dst []byte) (int, error) {
	clear(dst)
	if e := Available(); e != nil {
		return 0, e
	}
	return utf8Into(dst, func(p unsafe.Pointer, n uintptr) int32 { return loader.f.Xkb_keysym_get_name(sym, p, n) })
}
func KeysymFromName(name string) (uint32, error) {
	if e := Available(); e != nil {
		return 0, e
	}
	if e := validString(name); e != nil {
		return 0, e
	}
	b := cstr(name)
	v := loader.f.Xkb_keysym_from_name(ptr(b), 0)
	runtime.KeepAlive(b)
	return v, nil
}

type ComposeTable struct {
	mu         sync.Mutex
	h, context uintptr
}

func (c *Context) NewComposeTable(locale string) (*ComposeTable, error) {
	if e := validString(locale); e != nil {
		return nil, e
	}
	ctx, e := c.retain()
	if e != nil {
		return nil, e
	}
	b := cstr(locale)
	h := loader.f.Xkb_compose_table_new_from_locale(ctx, ptr(b), 0)
	runtime.KeepAlive(b)
	if h == 0 {
		releaseContext(ctx)
		return nil, fmt.Errorf("xkbcommon: no compose table for %q", locale)
	}
	return &ComposeTable{h: h, context: ctx}, nil
}
func (t *ComposeTable) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	h, ctx := t.h, t.context
	t.h = 0
	t.context = 0
	if h != 0 {
		loader.f.Xkb_compose_table_unref(h)
	}
	t.mu.Unlock()
	if ctx != 0 {
		releaseContext(ctx)
	}
	return nil
}
func (t *ComposeTable) NewState() (*ComposeState, error) {
	if t == nil {
		return nil, ErrClosed
	}
	t.mu.Lock()
	if t.h == 0 {
		t.mu.Unlock()
		return nil, ErrClosed
	}
	table := loader.f.Xkb_compose_table_ref(t.h)
	t.mu.Unlock()
	h := loader.f.Xkb_compose_state_new(table, 0)
	if h == 0 {
		loader.f.Xkb_compose_table_unref(table)
		return nil, errors.New("xkbcommon: create compose state failed")
	}
	return &ComposeState{h: h, table: table}, nil
}

type ComposeState struct {
	mu       sync.Mutex
	h, table uintptr
}

func (s *ComposeState) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	h, t := s.h, s.table
	s.h = 0
	s.table = 0
	if h != 0 {
		loader.f.Xkb_compose_state_unref(h)
	}
	s.mu.Unlock()
	if t != 0 {
		loader.f.Xkb_compose_table_unref(t)
	}
	return nil
}
func (s *ComposeState) Feed(sym uint32) (uint32, error) {
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return loader.f.Xkb_compose_state_feed(s.h, sym), nil
}
func (s *ComposeState) Status() (uint32, error) {
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return loader.f.Xkb_compose_state_get_status(s.h), nil
}

// UTF8Into is the compose counterpart of State.UTF8Into, with identical
// bounds, ownership, NUL and short-buffer semantics. It does not reset compose.
func (s *ComposeState) UTF8Into(dst []byte) (int, error) {
	clear(dst)
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return utf8Into(dst, func(p unsafe.Pointer, n uintptr) int32 { return loader.f.Xkb_compose_state_get_utf8(s.h, p, n) })
}

func (s *ComposeState) UTF8() (string, error) {
	if s == nil {
		return "", ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return "", ErrClosed
	}
	return sizedUTF8(func(p unsafe.Pointer, n uintptr) int32 { return loader.f.Xkb_compose_state_get_utf8(s.h, p, n) })
}
func (s *ComposeState) KeySym() (uint32, error) {
	if s == nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return 0, ErrClosed
	}
	return loader.f.Xkb_compose_state_get_one_sym(s.h), nil
}
func (s *ComposeState) Reset() error {
	if s == nil {
		return ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == 0 {
		return ErrClosed
	}
	loader.f.Xkb_compose_state_reset(s.h)
	return nil
}
