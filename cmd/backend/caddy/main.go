package main

import (
	"fmt"

	caddy "github.com/caddyserver/caddy/v2"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard" // Caddy standard modules

	// _ "github.com/caddyserver/json5-adapter"             // Caddy JSON5 config adapter

	"github.com/damnever/goodog"
	_ "github.com/damnever/goodog/backend/caddy" // Caddy module: http.handlers.goodog
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
