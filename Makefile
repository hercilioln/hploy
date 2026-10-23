BIN    := bin/hploy
VERSAO := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hercilioln/hploy/internal/cli.Version=$(VERSAO)

.PHONY: build run install fmt vet test clean

build:
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/hploy
	@echo "-> $(BIN) ($$(du -h $(BIN) | cut -f1))"

run: build
	@$(BIN) $(ARGS)

# Instala em ~/.local/bin, que normalmente ja esta no PATH.
install: build
	@mkdir -p $(HOME)/.local/bin
	install -m 0755 $(BIN) $(HOME)/.local/bin/hploy
	@echo "instalado em $(HOME)/.local/bin/hploy"
	@grep -q 'alias hp=' $(HOME)/.zshrc 2>/dev/null \
		&& echo "atalho 'hp' ja configurado" \
		|| echo "para o atalho:  echo 'alias hp=\"hploy\"' >> ~/.zshrc && source ~/.zshrc"

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin
