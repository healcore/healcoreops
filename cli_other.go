//go:build !windows

package main

import (
	"io"
	"os"
)

func attachConsole() io.Writer { return os.Stdout }
