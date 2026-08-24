package main

import (
	"os"

	argusdevapp "github.com/kakj-go/Argus/internal/app/argusdev"
)

func main() {
	os.Exit(argusdevapp.Run(os.Args[1:], os.Stdout, os.Stderr))
}
