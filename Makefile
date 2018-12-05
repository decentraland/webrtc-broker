build:
	go build -o build/simulation ./cmd/simulation
	go build -o build/server ./cmd/server
	# TODO: TEMP for backwards compatibilty:
	go build -o communications-server-go ./cmd/server

test: build
	go test -v github.com/decentraland/communications-server-go/internal/worldcomm

bench:
	go test -bench . github.com/decentraland/communications-server-go/internal/worldcomm

fmt:
	gofmt -w .

version:
	git rev-parse HEAD

.PHONY: build test
