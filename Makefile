build:
	go build

test: build
	go test -v github.com/decentraland/communications-server-go/worldcomm

fmt:
	gofmt -w .

bench:
	go test -bench . github.com/decentraland/communications-server-go/worldcomm

.PHONY: build test
