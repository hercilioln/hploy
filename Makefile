BIN     := bin/hploy
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hercilioln/hploy/internal/cli.Version=$(VERSION)

.PHONY: build run install fmt vet test clean

build:
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/hploy
	@echo "-> $(BIN) ($$(du -h $(BIN) | cut -f1))"

run: build
	@$(BIN) $(ARGS)

# Installs into ~/.local/bin, which is usually already on PATH.
install: build
	@mkdir -p $(HOME)/.local/bin
	install -m 0755 $(BIN) $(HOME)/.local/bin/hploy
	@echo "installed at $(HOME)/.local/bin/hploy"
	@grep -q 'alias hp=' $(HOME)/.zshrc 2>/dev/null \
		&& echo "'hp' alias already configured" \
		|| echo "for the alias:  echo 'alias hp=\"hploy\"' >> ~/.zshrc && source ~/.zshrc"

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin
