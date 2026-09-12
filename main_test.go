package main

import (
	"testing"

	"github.com/coder/websocket"
)

func TestWebsocketDependency(t *testing.T) {
	if websocket.StatusCode(0) != 0 {
		t.Fatal("unexpected status code")
	}
}
