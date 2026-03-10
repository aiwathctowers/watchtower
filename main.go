package main

import (
	"os"

	"watchtower/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
