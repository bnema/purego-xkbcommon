# purego-xkbcommon

Go 1.27 bindings for Linux `libxkbcommon.so.0`, without cgo or X11. The safe package owns native references; the `raw` package exposes generated ABI constants, types, and function registrations. Install libxkbcommon at runtime and xkeyboard-config for rules-based keymaps. The loader reports missing libraries and required symbols via `Available`.

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

Pinned upstream headers and licenses are in `upstream/`; `upstream/METADATA` and `SHA256SUMS` identify their exact source. Run `GOWORK=off go generate ./...` to regenerate `raw/generated.go`; consumers do not run generation. The generator checks the selected ABI declarations against the headers. `github.com/bnema/purego v0.13.0-bnema.1` matches purego-vulkan and supports `Dlopen`, `Dlsym`, and `RegisterFunc` with cgo disabled.
