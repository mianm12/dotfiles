// Package main 提供 dot CLI 的进程入口。
package main

import (
	"os"

	"github.com/ghstlnx/dotfiles/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
