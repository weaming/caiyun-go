# printable 的 column 是 cgo + 预编译静态库，交叉编译要带 C 工具链，这里用 zig cc
ZIG ?= zig
BIN ?= caiyun
DIST ?= dist
GOFLAGS_BUILD = -trimpath -ldflags '-s -w'

SOURCES := $(wildcard *.go) page.html favicon.svg go.mod go.sum

$(DIST):
	mkdir -p $@

# linux/amd64（musl 静态，可直接丢进 alpine/scratch）
$(DIST)/$(BIN)-linux-amd64: $(SOURCES) | $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC="$(ZIG) cc -target x86_64-linux-musl" \
		go build $(GOFLAGS_BUILD) -o $@ .

# darwin/arm64（本机原生 cgo）
$(DIST)/$(BIN)-darwin-arm64: $(SOURCES) | $(DIST)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $(GOFLAGS_BUILD) -o $@ .

all: $(DIST)/$(BIN)-linux-amd64 $(DIST)/$(BIN)-darwin-arm64

