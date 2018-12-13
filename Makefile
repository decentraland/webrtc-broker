PROTOCOL_VERSION=0.0.0

build:
	go build -o build/simulation ./cmd/simulation
	go build -o build/server ./cmd/server
	# TODO: TEMP for backwards compatibilty:
	go build -o communications-server-go ./cmd/server

fetch-protocol:
	curl -o internal/worldcomm/worldcomm.pb.go "https://raw.githubusercontent.com/decentraland/communications-protocol/${PROTOCOL_VERSION}/go/worldcomm.pb.go"

test: build
	go test -v github.com/decentraland/communications-server-go/internal/worldcomm

bench:
	go test -bench . github.com/decentraland/communications-server-go/internal/worldcomm

fmt:
	gofmt -w .

version:
	git rev-parse HEAD

.PHONY: build test
