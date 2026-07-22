// Package main 提供 dot CLI 的进程入口。
package main

import (
	"os"

	"github.com/mianm12/dotfiles/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
