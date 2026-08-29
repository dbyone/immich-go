// Command smokert dials a running immich-go instance over Socket.IO
// with a bearer token and prints the events it receives. Live smoke
// utility, not part of the server.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	clientsocket "github.com/zishang520/socket.io/clients/socket/v3"
)

func main() {
	url := os.Args[1]
	token := os.Args[2]
	mopts := clientsocket.DefaultManagerOptions()
	mopts.SetPath("/api/socket.io")
	mopts.SetExtraHeaders(http.Header{"Authorization": {"Bearer " + token}})
	mgr := clientsocket.NewManager(url, mopts)
	s := mgr.Socket("/", nil)
	done := make(chan struct{}, 2)
	s.On("connect", func(...any) { fmt.Println("CONNECTED") })
	s.On("on_server_version", func(args ...any) {
		fmt.Println("on_server_version:", args[0])
		done <- struct{}{}
	})
	s.On("on_upload_success", func(args ...any) {
		fmt.Println("on_upload_success:", args[0])
		done <- struct{}{}
	})
	s.On("connect_error", func(args ...any) {
		fmt.Println("connect_error:", args)
		os.Exit(1)
	})
	s.Connect()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		fmt.Println("TIMEOUT waiting for on_server_version")
		os.Exit(1)
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		fmt.Println("TIMEOUT waiting for on_upload_success")
		os.Exit(1)
	}
	fmt.Println("SMOKE_OK")
}
