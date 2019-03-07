PROTOC ?= protoc

build:
	go build -o build/simulation ./cmd/simulation
	go build -o build/coordinator ./cmd/coordinator
	go build -o build/server ./cmd/server

compile-protocol:
	cd pkg/protocol; ${PROTOC} --js_out=import_style=commonjs,binary:. --ts_out=. --go_out=. ./commproto.proto

test: build
	go test -race $(TEST_FLAGS) \
github.com/decentraland/communications-server-go/internal/worldcomm \
github.com/decentraland/communications-server-go/internal/coordinator


cover: TEST_FLAGS=-coverprofile=coverage.out
cover: test

check-cover: cover
	go tool cover -html=coverage.out

test-integration: build
	go test -race -count=1 $(TEST_FLAGS) -tags=integration github.com/decentraland/communications-server-go/internal/simulation

vtest-integration: TEST_FLAGS=-v
vtest-integration: test-integration

vtest: TEST_FLAGS=-v
vtest: test

fmt:
	gofmt -w .
	goimports -w .

version:
	git rev-parse HEAD

todo:
	grep --include "*.go" -r TODO *

.PHONY: build test vtest
