build:
	go build -o build/simulation ./cmd/simulation
	go build -o build/coordinator ./cmd/coordinator
	go build -o build/server ./cmd/server

copy-protocol:
	cp ../communications-protocol/go/commproto.pb.go pkg/protocol/
	cp ../communications-protocol/commproto.proto pkg/protocol/

test: build
	go test $(TEST_FLAGS) \
github.com/decentraland/communications-server-go/internal/worldcomm \
github.com/decentraland/communications-server-go/internal/coordinator

cover: TEST_FLAGS=-coverprofile=coverage.out
cover: test

check-cover: cover
	go tool cover -html=coverage.out

test-integration: build
	go test -count=1 $(TEST_FLAGS) -tags=integration github.com/decentraland/communications-server-go/internal/simulation

vtest-integration: TEST_FLAGS=-v
vtest-integration: test-integration

vtest: TEST_FLAGS=-v
vtest: test

bench:
	go test -bench . github.com/decentraland/communications-server-go/internal/worldcomm

fmt:
	gofmt -w .
	goimports -w .

version:
	git rev-parse HEAD

todo:
	grep --include "*.go" -r TODO *

.PHONY: build test vtest
