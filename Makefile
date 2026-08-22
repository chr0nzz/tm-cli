VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE ?= $(shell date -u +%Y-%m-%d)
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
GOFLAGS = -trimpath -ldflags "$(LDFLAGS)"

.PHONY: build test vet fmt lint release-dry clean

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -o bin/tm ./cmd/tm

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	shellcheck -s sh setup.sh
	@for f in scripts/*.sh; do if [ -e "$$f" ]; then shellcheck "$$f"; fi; done

release-dry:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o dist/tm-linux-amd64 ./cmd/tm
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -o dist/tm-linux-arm64 ./cmd/tm
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build $(GOFLAGS) -o dist/tm-linux-armv7 ./cmd/tm
	cd dist && sha256sum tm-linux-* > SHA256SUMS

clean:
	rm -rf bin dist
