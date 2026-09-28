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
