package main

import (
	"github.com/bnema/purego-xkbcommon/internal/gen"
	"log"
)

func main() {
	if err := gen.Generate("."); err != nil {
		log.Fatal(err)
	}
}
