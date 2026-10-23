package main

import (
	"os"

	"github.com/Microck/lsops/internal/app"
)

var version = "0.3.0"

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
