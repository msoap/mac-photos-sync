BINARY := mac-photos-sync

.PHONY: build build-amd64 install test
build:
	go build -o $(BINARY) .
build-amd64:
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY)-amd64 .
install:
	go install .
test:
	go test ./...
