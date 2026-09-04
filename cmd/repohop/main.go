// Command repohop drives a set of git repositories as one unit.
package main

import "github.com/rustbohr/repohop/internal/cli"

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	cli.Execute(version)
}
