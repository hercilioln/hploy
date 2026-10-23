BIN     := bin/hploy
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hercilioln/hploy/internal/cli.Version=$(VERSION)

.PHONY: build run install uninstall fmt vet test clean

build:
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/hploy
	@echo "-> $(BIN) ($$(du -h $(BIN) | cut -f1))"

run: build
	@$(BIN) $(ARGS)

# Installs into ~/.local/bin, which is usually already on PATH.
# Override with: make install PREFIX=/usr/local/bin
PREFIX ?= $(HOME)/.local/bin

install: build
	@mkdir -p $(PREFIX)
	@install -m 0755 $(BIN) $(PREFIX)/hploy.new
	@mv -f $(PREFIX)/hploy.new $(PREFIX)/hploy
	@echo "installed at $(PREFIX)/hploy ($(VERSION))"
	@grep -q 'alias hp=' $(HOME)/.zshrc 2>/dev/null \
		&& echo "'hp' alias already configured" \
		|| echo "for the alias:  echo 'alias hp=\"hploy\"' >> ~/.zshrc && source ~/.zshrc"

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

test:
	go test ./...

uninstall:
	@rm -f $(PREFIX)/hploy
	@echo "removed $(PREFIX)/hploy"

clean:
	rm -rf bin
