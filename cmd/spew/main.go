package main

import (
	"context"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/kmwenja/spew"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: spew <config-file>\n")
		os.Exit(1)
	}

	var c spew.Config
	_, err := toml.DecodeFile(os.Args[1], &c)
	if err != nil {
		panic(fmt.Errorf("could not read from config file: %w", err))
	}

	err = spew.Spew(context.Background(), c, os.Stdout)
	if err != nil {
		panic(err)
	}
}
