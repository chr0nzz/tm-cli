package main

import (
	"os"

	"github.com/chr0nzz/tm-cli/internal/cli"
)

var version = "dev"
var commit = ""
var date = ""

func main() {
	os.Exit(cli.Main(version, commit, date))
}
