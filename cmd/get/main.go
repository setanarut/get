package main

import (
	"context"
	"fmt"
	"os"

	"github.com/setanarut/get"
)

var version string

func main() {
	cli := get.New()
	if err := cli.Run(context.Background(), version, os.Args[1:]); err != nil {
		if cli.Trace {
			fmt.Fprintf(os.Stderr, "Error:\n%+v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Error:\n  %v\n", err)
		}
		os.Exit(1)
	}
}
