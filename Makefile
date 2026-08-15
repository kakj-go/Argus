.PHONY: fmt test vet run-server

fmt:
	rg --files cmd internal -g '*.go' | xargs gofmt -w

test:
	go test ./...

vet:
	go vet ./...

run-server:
	go run ./cmd/argus-server
