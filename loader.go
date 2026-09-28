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

// Available resolves the SONAME and every required symbol, reporting the first
// incompatible symbol rather than failing later in a keyboard event callback.
func Available() error {
	loader.Do(func() {
		lib, err := purego.Dlopen("libxkbcommon.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			loader.err = fmt.Errorf("xkbcommon: load libxkbcommon.so.0: %w", err)
			return
		}
		addresses := make(map[string]uintptr, len(raw.Symbols))
		for _, name := range raw.Symbols {
			address, e := purego.Dlsym(lib, name)
			if e != nil || address == 0 {
				loader.err = fmt.Errorf("xkbcommon: incompatible libxkbcommon.so.0: missing %s: %v", name, e)
				_ = purego.Dlclose(lib)
				return
			}
			addresses[name] = address
		}
		raw.Register(&loader.f, addresses, purego.RegisterFunc)
		// Intentionally keep the library open: registered function pointers and live
		// native objects remain valid for the process lifetime.
	})
	return loader.err
}
