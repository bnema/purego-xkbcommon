package xkbcommon

import (
	"fmt"
	"sync"

	"github.com/bnema/purego"
	"github.com/bnema/purego-xkbcommon/raw"
)

var loader struct {
	sync.Once
	f   raw.Funcs
	err error
}

// resolveLibrary keeps path and required symbols injectable for diagnostic tests.
// On success it leaves the library open for the lifetime of resolved function addresses.
func resolveLibrary(path string, symbols []string) (map[string]uintptr, error) {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("xkbcommon: load %s: %w", path, err)
	}
	addresses := make(map[string]uintptr, len(symbols))
	for _, name := range symbols {
		address, e := purego.Dlsym(lib, name)
		if e != nil || address == 0 {
			_ = purego.Dlclose(lib)
			return nil, fmt.Errorf("xkbcommon: incompatible %s: missing %s: %v", path, name, e)
		}
		addresses[name] = address
	}
	return addresses, nil
}

// Available resolves the SONAME and every required symbol, reporting the first
// incompatible symbol rather than failing later in a keyboard event callback.
func Available() error {
	loader.Do(func() {
		addresses, err := resolveLibrary("libxkbcommon.so.0", raw.Symbols)
		if err != nil {
			loader.err = err
			return
		}
		raw.Register(&loader.f, addresses)
		// Intentionally keep the library open: resolved function addresses and live
		// native objects remain valid for the process lifetime.
	})
	return loader.err
}
