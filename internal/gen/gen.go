// Package gen generates the deliberately bounded libxkbcommon ABI subset.
package gen

import (
	"fmt"
	"go/format"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var comments = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)

func clean(s string) string { return comments.ReplaceAllString(s, "") }
func declared(h, name string) bool {
	re := regexp.MustCompile(`(?m)\b` + regexp.QuoteMeta(name) + `\s*\([^;{}]*\)\s*;`)
	return re.MatchString(clean(h))
}

var def = regexp.MustCompile(`(?m)^\s*#define\s+(XKB_(?:KEY_|MOD_NAME_|VMOD_NAME_|LED_NAME_)[A-Za-z0-9_]+)\s+("[^"]*"|0x[0-9a-fA-F]+|[0-9]+)\b?`)
var en = regexp.MustCompile(`(?s)enum\s+xkb_[a-z_]+\s*\{([^}]*)\}`)
var item = regexp.MustCompile(`^\s*(XKB_[A-Z][A-Z0-9_]*)(?:\s*=\s*(.+))?$`)

func constants(h string) string {
	h = clean(h)
	var b strings.Builder
	for _, e := range en.FindAllStringSubmatch(h, -1) {
		last := int64(-1)
		for _, part := range strings.Split(e[1], ",") {
			m := item.FindStringSubmatch(strings.TrimSpace(part))
			if m == nil {
				continue
			}
			expr := strings.TrimSpace(m[2])
			if expr == "" {
				last++
				expr = strconv.FormatInt(last, 10)
			} else if n, err := strconv.ParseInt(expr, 0, 64); err == nil {
				last = n
			} else if v := regexp.MustCompile(`^\(1 << (\d+)\)$`).FindStringSubmatch(expr); v != nil {
				shift, _ := strconv.Atoi(v[1])
				last = 1 << shift
			} else {
				continue
			}
			fmt.Fprintf(&b, "%s = %s\n", m[1], expr)
		}
	}
	for _, m := range def.FindAllStringSubmatch(h, -1) {
		fmt.Fprintf(&b, "%s = %s\n", m[1], m[2])
	}
	return b.String()
}

// Functions describes the bound ABI; each name is checked against the pinned headers.
// Pointers are uintptr to avoid GC passing Go pointers to native code unexpectedly.
var Functions = []struct{ Name, Signature string }{
	{"xkb_context_new", "func(uint32) uintptr"}, {"xkb_context_ref", "func(uintptr) uintptr"}, {"xkb_context_unref", "func(uintptr)"},
	{"xkb_keymap_new_from_names", "func(uintptr, unsafe.Pointer, uint32) uintptr"}, {"xkb_keymap_new_from_string", "func(uintptr, unsafe.Pointer, uint32, uint32) uintptr"}, {"xkb_keymap_new_from_buffer", "func(uintptr, unsafe.Pointer, uintptr, uint32, uint32) uintptr"},
	{"xkb_keymap_ref", "func(uintptr) uintptr"}, {"xkb_keymap_unref", "func(uintptr)"}, {"xkb_keymap_get_as_string", "func(uintptr,uint32) uintptr"},
	{"xkb_keymap_mod_get_index", "func(uintptr,unsafe.Pointer) uint32"}, {"xkb_keymap_key_repeats", "func(uintptr,uint32) int32"}, {"xkb_keymap_num_layouts", "func(uintptr) uint32"},
	{"xkb_state_new", "func(uintptr) uintptr"}, {"xkb_state_ref", "func(uintptr) uintptr"}, {"xkb_state_unref", "func(uintptr)"},
	{"xkb_state_update_mask", "func(uintptr,uint32,uint32,uint32,uint32,uint32,uint32) uint32"},
	{"xkb_state_serialize_layout", "func(uintptr,uint32) uint32"}, {"xkb_state_serialize_mods", "func(uintptr,uint32) uint32"},
	{"xkb_state_mod_name_is_active", "func(uintptr,unsafe.Pointer,uint32) int32"}, {"xkb_state_mod_index_is_active", "func(uintptr,uint32,uint32) int32"},
	{"xkb_state_key_get_one_sym", "func(uintptr,uint32) uint32"}, {"xkb_state_key_get_utf8", "func(uintptr,uint32,unsafe.Pointer,uintptr) int32"},
	{"xkb_keysym_get_name", "func(uint32,unsafe.Pointer,uintptr) int32"}, {"xkb_keysym_from_name", "func(unsafe.Pointer,uint32) uint32"},
	{"xkb_compose_table_new_from_locale", "func(uintptr,unsafe.Pointer,uint32) uintptr"}, {"xkb_compose_table_ref", "func(uintptr) uintptr"}, {"xkb_compose_table_unref", "func(uintptr)"},
	{"xkb_compose_state_new", "func(uintptr,uint32) uintptr"}, {"xkb_compose_state_ref", "func(uintptr) uintptr"}, {"xkb_compose_state_unref", "func(uintptr)"},
	{"xkb_compose_state_feed", "func(uintptr,uint32) uint32"}, {"xkb_compose_state_get_status", "func(uintptr) uint32"},
	{"xkb_compose_state_reset", "func(uintptr)"}, {"xkb_compose_state_get_utf8", "func(uintptr,unsafe.Pointer,uintptr) int32"}, {"xkb_compose_state_get_one_sym", "func(uintptr) uint32"},
}

func field(name string) string { return strings.ToUpper(name[:1]) + name[1:] }

// Only fixed-width integer-class values reach Syscall6/Syscall15: floats, bools,
// varargs and structs would be silently mis-passed, so the generator rejects them.
// unsafe.Pointer is accepted for parameters only (Go memory handed to C);
// uintptr is for C-owned handles and sizes.
var allowedParams = map[string]bool{"uintptr": true, "uint32": true, "int32": true, "unsafe.Pointer": true}
var allowedReturns = map[string]bool{"": true, "uintptr": true, "uint32": true, "int32": true}

// method renders an exported, allocation-free wrapper calling the resolved
// address through purego.Syscall6 (up to 6 arguments) or Syscall15.
func method(name, sig string) (string, error) {
	rest, ok := strings.CutPrefix(sig, "func(")
	if !ok {
		return "", fmt.Errorf("bad signature for %s: %s", name, sig)
	}
	params, ret, _ := strings.Cut(rest, ")")
	ret = strings.TrimSpace(ret)
	var types []string
	if strings.TrimSpace(params) != "" {
		for _, p := range strings.Split(params, ",") {
			types = append(types, strings.TrimSpace(p))
		}
	}
	if len(types) > 15 {
		return "", fmt.Errorf("%s: more than 15 arguments", name)
	}
	if !allowedReturns[ret] {
		return "", fmt.Errorf("%s: unsupported return type %q", name, ret)
	}
	var decl, args []string
	for i, t := range types {
		if !allowedParams[t] {
			return "", fmt.Errorf("%s: unsupported parameter type %q", name, t)
		}
		decl = append(decl, fmt.Sprintf("a%d %s", i+1, t))
		// The uintptr conversion must stay inside the Syscall call expression so
		// //go:uintptrescapes keeps unsafe.Pointer arguments alive and unmoved.
		if t == "uintptr" {
			args = append(args, fmt.Sprintf("a%d", i+1))
		} else {
			args = append(args, fmt.Sprintf("uintptr(a%d)", i+1))
		}
	}
	call, size := "purego.Syscall6", 6
	if len(types) > 6 {
		call, size = "purego.Syscall15", 15
	}
	for len(args) < size {
		args = append(args, "0")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n// %s calls %s without allocating.\nfunc (f *Funcs) %s(%s) %s {\n", field(name), name, field(name), strings.Join(decl, ", "), ret)
	switch ret {
	case "":
		fmt.Fprintf(&b, "%s(f.%s, %s)\n", call, name, strings.Join(args, ", "))
	case "uintptr":
		fmt.Fprintf(&b, "r, _, _ := %s(f.%s, %s)\nreturn r\n", call, name, strings.Join(args, ", "))
	default:
		fmt.Fprintf(&b, "r, _, _ := %s(f.%s, %s)\nreturn %s(r)\n", call, name, strings.Join(args, ", "), ret)
	}
	b.WriteString("}\n")
	return b.String(), nil
}
func Generate(root string) error {
	h1, err := os.ReadFile(root + "/upstream/xkbcommon.h")
	if err != nil {
		return err
	}
	h2, err := os.ReadFile(root + "/upstream/xkbcommon-compose.h")
	if err != nil {
		return err
	}
	hdr := string(h1) + "\n" + string(h2)
	var b strings.Builder
	b.WriteString("// Code generated by go generate; DO NOT EDIT.\npackage raw\n\nimport (\n\"unsafe\"\n\n\"github.com/bnema/purego\"\n)\n\n")
	b.WriteString("// Opaque C handles and scalar typedefs.\ntype Context uintptr\ntype Keymap uintptr\ntype State uintptr\ntype ComposeTable uintptr\ntype ComposeState uintptr\ntype Keycode uint32\ntype Keysym uint32\ntype LayoutIndex uint32\ntype ModIndex uint32\ntype ModMask uint32\ntype LayoutMask uint32\n")
	b.WriteString("// RuleNames matches struct xkb_rule_names (five const char pointers). The\n// fields are unsafe.Pointer so the GC sees the NUL-terminated strings they reference.\ntype RuleNames struct { Rules, Model, Layout, Variant, Options unsafe.Pointer }\n")
	b.WriteString("const (\n" + constants(hdr))
	for _, f := range []string{"xkbcommon-keysyms.h", "xkbcommon-names.h"} {
		data, e := os.ReadFile(root + "/upstream/" + f)
		if e != nil {
			return e
		}
		b.WriteString(constants(string(data)))
	}
	b.WriteString(")\n\n// Funcs holds resolved library function addresses. Each function is invoked\n// through purego.Syscall6/Syscall15 (fixed arity, no reflection, no allocation).\ntype Funcs struct {\n")
	for _, f := range Functions {
		if !declared(hdr, f.Name) {
			return fmt.Errorf("missing pinned declaration: %s", f.Name)
		}
		fmt.Fprintf(&b, "%s uintptr\n", f.Name)
	}
	b.WriteString("}\n\n// Symbols is the complete required symbol set.\nvar Symbols = []string{\n")
	for _, f := range Functions {
		fmt.Fprintf(&b, "%q,\n", f.Name)
	}
	b.WriteString("}\n\n// Register stores prevalidated symbol addresses in a Funcs value.\nfunc Register(f *Funcs, addresses map[string]uintptr) {\n")
	for _, f := range Functions {
		fmt.Fprintf(&b, "f.%s = addresses[%q]\n", f.Name, f.Name)
	}
	b.WriteString("}\n")
	for _, f := range Functions {
		m, err := method(f.Name, f.Signature)
		if err != nil {
			return err
		}
		b.WriteString(m)
	}
	s := b.String()
	formatted, err := format.Source([]byte(s))
	if err != nil {
		return fmt.Errorf("format generated file: %w", err)
	}
	return os.WriteFile(root+"/raw/generated.go", formatted, 0644)
}
