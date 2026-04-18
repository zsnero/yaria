BINARY = yaria
CMD = ./cmd/yaria
VERSION = 1.0.0
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build build-pro run install clean tidy vet

# Community build (open-source, download only)
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

# Pro build (download + Mantorex, closed-source)
build-pro:
	go build -tags pro -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

run:
	go run $(CMD)

run-pro:
	go run -tags pro $(CMD)

install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

install-pro:
	go install -tags pro -ldflags "$(LDFLAGS)" $(CMD)

clean:
	rm -f $(BINARY) $(BINARY)-*
	go clean

tidy:
	go mod tidy

vet:
	go vet ./...

vet-pro:
	go vet -tags pro ./...

# Cross-compilation: Community (open-source)
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-linux-amd64 $(CMD)

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-windows-amd64.exe $(CMD)

build-mac:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-darwin-amd64 $(CMD)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-darwin-arm64 $(CMD)

build-all: build-linux build-windows build-mac

# Cross-compilation: Pro (closed-source, for GitHub Releases)
build-pro-linux:
	GOOS=linux GOARCH=amd64 go build -tags pro -ldflags "$(LDFLAGS)" -o $(BINARY)-pro-linux-amd64 $(CMD)

build-pro-windows:
	GOOS=windows GOARCH=amd64 go build -tags pro -ldflags "$(LDFLAGS)" -o $(BINARY)-pro-windows-amd64.exe $(CMD)

build-pro-mac:
	GOOS=darwin GOARCH=amd64 go build -tags pro -ldflags "$(LDFLAGS)" -o $(BINARY)-pro-darwin-amd64 $(CMD)
	GOOS=darwin GOARCH=arm64 go build -tags pro -ldflags "$(LDFLAGS)" -o $(BINARY)-pro-darwin-arm64 $(CMD)

build-pro-all: build-pro-linux build-pro-windows build-pro-mac
