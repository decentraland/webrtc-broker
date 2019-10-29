PROTOC ?= protoc
DEBUG_FLAGS = -gcflags="all=-N -l"

build:
	go build -o build/simulation ./cmd/simulation
	go build -o build/coordinator ./cmd/coordinator
	go build -o build/broker ./cmd/broker

buildtest:
	go build -o build/densetest ./cmd/perftest/densetest

buildall: build buildtest

escape:
	go build -gcflags -m github.com/decentraland/webrtc-broker/pkg/coordinator 2> build/escape.out
	go build -gcflags -m github.com/decentraland/webrtc-broker/pkg/server 2>> build/escape.out
	cat build/escape.out | grep -v inlining | grep -v "does not escape" | grep -v "can inline"

compile-protocol:
	cd pkg/protocol; ${PROTOC} --js_out=import_style=commonjs,binary:. --ts_out=. --go_out=. ./broker.proto

test: build
	go test -count=1 -race $(TEST_FLAGS) \
github.com/decentraland/webrtc-broker/pkg/server \
github.com/decentraland/webrtc-broker/pkg/broker \
github.com/decentraland/webrtc-broker/pkg/coordinator

integration:
	go test -v -race -count=1 $(TEST_FLAGS) -tags=integration github.com/decentraland/webrtc-broker/pkg/simulation

bench: build
	go test -bench=. -run="NOTHING" github.com/decentraland/webrtc-broker/pkg/server

cover: TEST_FLAGS=-coverprofile=coverage.out
cover: test

check-cover: cover
	go tool cover -html=coverage.out

fmt:
	gofmt -w -s .
	goimports -w .

version:
	git rev-parse HEAD

tidy:
	go mod tidy

todo:
	grep --include "*.go" -r TODO *

lint:
	golint ./...

lintci:
	golangci-lint run ./...

validateci:
	circleci config validate

installtools:
	GO111MODULE=off go get -u -v github.com/davecheney/gcvis
	GO111MODULE=off go get -u github.com/golangci/golangci-lint/cmd/golangci-lint
	GO111MODULE=off go get -u golang.org/x/lint/golint

.PHONY: build test integration cover
