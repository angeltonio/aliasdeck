package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/angeltonio/aliasdeck/internal/releasecheck"
)

func main() {
	version := flag.String("version", "", "release version tag")
	sourceRef := flag.String("source-ref", "", "Git ref used as release source")
	flag.Parse()
	if err := releasecheck.Validate(*version, *sourceRef); err != nil {
		fmt.Fprintln(os.Stderr, "release binding:", err)
		os.Exit(1)
	}
	fmt.Printf("release binding verified: %s from %s\n", *version, *sourceRef)
}
