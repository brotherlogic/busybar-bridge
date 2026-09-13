package main

import (
	"testing"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

func TestWebsocketDependency(t *testing.T) {
	if websocket.StatusCode(0) != 0 {
		t.Fatal("unexpected status code")
	}
}

func TestProtobufDependency(t *testing.T) {
	var m proto.Message
	if proto.Size(m) != 0 {
		t.Fatal("unexpected proto size")
	}
}
