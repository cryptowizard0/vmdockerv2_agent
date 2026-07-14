package main

import (
	"os"

	"github.com/cryptowizard0/vmdockerv2_agent/server"
)

func main() {
	// os.Args[1:] is the adapter's command — the image CMD appended after the
	// ENTRYPOINT adapter. Empty when the module declares no CMD, in which case
	// the supervisor falls back to the user-startup hook.
	srv := server.New(8080, os.Args[1:])
	srv.Run()
}
