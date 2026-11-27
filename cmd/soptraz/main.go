package main

import (
	"os"

	"github.com/JavierGrecco/SOPTRAZ/internal/app/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
