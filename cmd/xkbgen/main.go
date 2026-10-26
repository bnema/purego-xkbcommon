package main

import (
	"bnema.dev/purego-xkbcommon/internal/gen"
	"log"
)

func main() {
	if err := gen.Generate("."); err != nil {
		log.Fatal(err)
	}
}
