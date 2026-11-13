// Command spin-ssh picks a SpinupWP site and SSHes to it as the site user.
package main

import "github.com/marcossantiago/spin-ssh/internal/cli"

// version is set at release time: -ldflags "-X main.version=v1.2.3".
var version string

func main() { cli.Main(version) }
