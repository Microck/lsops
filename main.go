package main

import (
	"os"

	"github.com/Microck/lsops/internal/app"
)

var version = "0.2.0"

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
