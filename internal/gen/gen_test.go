package gen

import (
	"strings"
	"testing"
)

func TestDeclarations(t *testing.T) {
	h := "/* xkb_fake(void); */\nXKB_EXPORT struct xkb_context *\nxkb_context_new(enum xkb_context_flags flags);"
	if !declared(h, "xkb_context_new") || declared(h, "xkb_fake") {
		t.Fatal("incorrect declaration selection")
	}
	if declared(h, "xkb_context_unref") {
		t.Fatal("missing declaration accepted")
	}
}
func TestConstants(t *testing.T) {
	h := "enum xkb_test { XKB_A = (1 << 2), XKB_B, XKB_C = 0x10 };\n#define XKB_MOD_NAME_SHIFT \"Shift\"\n#define XKB_KEY_dead_acute 0xfe51\n"
	got := constants(h)
	for _, want := range []string{"XKB_A = (1 << 2)", "XKB_B = 5", "XKB_C = 0x10", "XKB_MOD_NAME_SHIFT = \"Shift\"", "XKB_KEY_dead_acute = 0xfe51"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
}

func TestMethodWhitelist(t *testing.T) {
	for _, sig := range []string{"func(float64) uint32", "func(bool)", "func(uintptr, ...uintptr) uint32", "func(uint32) float32", "func(uint32) unsafe.Pointer", "func(*int)", "func(uint32) bool"} {
		if _, err := method("xkb_bad", sig); err == nil {
			t.Fatalf("accepted %s", sig)
		}
	}
	got, err := method("xkb_ok", "func(uintptr, unsafe.Pointer, int32) uint32")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"purego.Syscall6(f.xkb_ok, a1, uintptr(a2), uintptr(a3), 0, 0, 0)", "return uint32(r)", "a2 unsafe.Pointer"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if got, _ = method("xkb_seven", "func(uintptr,uint32,uint32,uint32,uint32,uint32,uint32)"); !strings.Contains(got, "purego.Syscall15(") {
		t.Fatal("seven args must use Syscall15")
	}
}
