package main

import (
	"os"

	"github.com/chr0nzz/traefik-stack/internal/cli"
)

var version = "dev"
var commit = ""
var date = ""

func main() {
	os.Exit(cli.Main(version, commit, date))
}
