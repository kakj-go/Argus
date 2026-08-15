package main

import (
	"os"

	argusctlapp "github.com/kakj-go/Argus/internal/app/argusctl"
)

func main() {
	os.Exit(argusctlapp.Run(os.Args[1:], os.Stdout, os.Stderr))
}
