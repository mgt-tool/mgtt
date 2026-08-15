// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"os"

	"github.com/mgt-tool/mgtt/internal/cli"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "mgtt: internal error: %v\nThis is a bug. Please report it at https://github.com/mgt-tool/mgtt/issues\n", r)
			os.Exit(3)
		}
	}()
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
