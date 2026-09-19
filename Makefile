BUSYBAR_PROTOBUF_REPO ?= https://github.com/busy-app/busybar-protobuf.git
BUSYBAR_PROTOBUF_REF ?= dev
PROTO_DIR ?= proto
PB_DIR ?= pkg/pb
BUSYBAR_PROTO_DIR ?= $(PROTO_DIR)/busybar

.PHONY: all proto-sync proto-gen test build clean

all: proto-gen test build

proto-sync:
	@echo "Syncing upstream protobuf schemas from $(BUSYBAR_PROTOBUF_REPO) ($(BUSYBAR_PROTOBUF_REF))..."
	@rm -rf .tmp-busybar-protobuf
	git clone --depth 1 -b $(BUSYBAR_PROTOBUF_REF) $(BUSYBAR_PROTOBUF_REPO) .tmp-busybar-protobuf
	mkdir -p $(BUSYBAR_PROTO_DIR)/state $(BUSYBAR_PROTO_DIR)/util
	cp .tmp-busybar-protobuf/*.proto $(BUSYBAR_PROTO_DIR)/
	cp .tmp-busybar-protobuf/state/*.proto $(BUSYBAR_PROTO_DIR)/state/
	cp .tmp-busybar-protobuf/util/*.proto $(BUSYBAR_PROTO_DIR)/util/
	@rm -rf .tmp-busybar-protobuf
	@for f in $$(find $(BUSYBAR_PROTO_DIR) -name "*.proto"); do \
		if ! grep -q "option go_package" "$$f"; then \
			sed -i '/syntax = "proto3";/a option go_package = "github.com/brotherlogic/busybar-bridge/pkg/pb/busybar;busybar";' "$$f"; \
		fi \
	done
	@echo "Protobuf schemas synchronized successfully."

proto-gen:
	@echo "Generating Go protobuf code..."
	mkdir -p $(PB_DIR)/busybar
	protoc -I=$(BUSYBAR_PROTO_DIR) --go_out=. --go_opt=module=github.com/brotherlogic/busybar-bridge $$(find $(BUSYBAR_PROTO_DIR) -name "*.proto")
	protoc -I=$(PROTO_DIR) --go_out=. --go_opt=module=github.com/brotherlogic/busybar-bridge $(PROTO_DIR)/event.proto
	protoc -I=$(PROTO_DIR) --go_out=. --go_opt=module=github.com/brotherlogic/busybar-bridge $(PROTO_DIR)/calendar.proto
	@echo "Protobuf Go bindings generated in $(PB_DIR)."

test:
	go test -v ./...

build:
	go build ./...
