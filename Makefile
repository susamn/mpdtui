BINARY := mpdtui
CMD    := ./cmd/mpdtui

.PHONY: build test vet fmt clean install

build:
	go build -o $(BINARY) -ldflags "-s -w" $(CMD)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

clean:
	rm -f $(BINARY)

install:
	go install $(CMD)
