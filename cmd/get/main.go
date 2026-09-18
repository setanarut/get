package main

import (
	"context"
	"fmt"
	"os"

	"github.com/setanarut/get"
)

var version = "v1.1.3"

func main() {
	cli := get.New()
	if err := cli.Run(context.Background(), version, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
