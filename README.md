# purego-xkbcommon

Go 1.27 bindings for Linux `libxkbcommon.so.0`, without cgo or X11. The safe package owns native references; the `raw` package exposes generated ABI constants, types, and function bindings. Install libxkbcommon at runtime and xkeyboard-config for rules-based keymaps. The loader reports missing libraries and required symbols via `Available`.

```go
ctx, err := xkbcommon.NewContext()
if err != nil { return err }
defer ctx.Close()
keymap, err := ctx.NewKeymapRules("evdev", "pc105", "fr", "", "")
if err != nil { return err }
defer keymap.Close()
state, err := keymap.NewState()
if err != nil { return err }
defer state.Close()
text, err := state.UTF8(xkbcommon.WaylandKeycode(evdevKeycode))
```

Use `NewKeymapFD(fd, size)` for Wayland `xkb_v1` keymaps; it copies exactly `size` bytes with `pread` (at most 16 MiB), fails on a short read, and accepts an optional trailing NUL. **The caller retains and must close the FD**, on success and failure. `UpdateMask` accepts Wayland modifier masks and group indices. `ComposeState.Feed` and `Status` distinguish composing, composed, cancelled, and ignored keysyms; use `UTF8` only for a composed result. Objects retain their native parents; close each object, in any order. Do not copy value structs containing a mutex.

`State.UTF8Into` and `ComposeState.UTF8Into` write into a caller-owned buffer of 1..`MaxUTF8Buffer` bytes and create no Go string. On `io.ErrShortBuffer` the whole buffer is wiped and n+1 bytes are needed; the caller wipes the buffer after use.

Pinned upstream headers and licenses are in `upstream/`; `upstream/METADATA` and `SHA256SUMS` identify their exact source. Run `GOWORK=off go generate ./...` to regenerate `raw/generated.go`; consumers do not run generation. The generator checks the selected ABI declarations against the headers. `github.com/bnema/purego v0.13.0-bnema.1` matches purego-vulkan and supports `Dlopen`, `Dlsym`, and the fixed-arity `Syscall6`/`Syscall15` with cgo disabled.

## Allocation-free hot path

`raw.Funcs` holds symbol addresses resolved once by `Available`; every generated method calls `purego.Syscall6` (or `Syscall15` for more than six arguments) directly, so no reflection or return boxing happens per call. Per-key calls — `State.KeySym`, `State.UTF8Into`, `State.UpdateMask`, `State.Layout`, `State.Mods`, `State.ModIndexActive`, `State.ModNameActive`, `Keymap.KeyRepeats`, `KeysymNameInto` and the `ComposeState` `Feed`/`Status`/`KeySym`/`UTF8Into`/`Reset` methods — perform 0 allocations (asserted by `TestHotPathAllocs`). The `*Into` methods stay allocation-free only with a long-lived `dst` reused across calls: a buffer passed to C escapes, so a fresh local array costs one allocation per call. The string-returning `UTF8` and `KeysymName` still allocate their result.


## v0.2.0: `raw` API change

Only the `raw` package changed incompatibly; the root `xkbcommon` package is source-compatible (it gains `State.ModNameActive`, `State.ModIndexActive`, `Keymap.KeyRepeats` and `KeysymNameInto`).

- `raw.Funcs` fields are no longer `func` values. Each function is now a method with the same exported name (`f.Xkb_state_key_get_one_sym(...)`), and the struct holds unexported symbol addresses.
- Pointer parameters (strings, buffers, `*RuleNames`) are `unsafe.Pointer` instead of `uintptr`, and `RuleNames` fields are `unsafe.Pointer`. Pass the pointer directly; never pre-convert it to `uintptr`, which would not keep the memory alive.
- `raw.Register(f, addresses, register)` is now `raw.Register(f, addresses)`; there is no `purego.RegisterFunc` hook.
- A zero `raw.Funcs` is unusable: calling any method panics (`purego: fn is nil`) until `Register` has filled it. `xkbcommon.Available` does this for the package's own loader.
- The generator only accepts `uintptr`, `uint32`, `int32` and `unsafe.Pointer` parameters and `uintptr`, `uint32`, `int32` or no return value, and rejects floats, bools and varargs.
