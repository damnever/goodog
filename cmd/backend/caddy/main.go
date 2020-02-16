package main

import (
	"fmt"

	caddy "github.com/caddyserver/caddy/v2"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"

	// Plug in Caddy modules here
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	"github.com/damnever/goodog"
	_ "github.com/damnever/goodog/backend/caddy"
)

func main() {
	caddycmd.RegisterCommand(caddycmd.Command{
		Name: "version-goodog",
		Func: func(caddycmd.Flags) (int, error) {
			fmt.Println(goodog.VersionInfo())
			return caddy.ExitCodeSuccess, nil
		},
		Short: "Prints the version of goodog",
	})
	caddycmd.Main()
}
