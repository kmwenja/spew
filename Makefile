PREFIX ?= "/usr/bin/"

all: build

build:
	mkdir -p bin
	go build -o bin/spew ./cmd/spew

run: build
	bin/spew bin/test.toml

install: build
	install -m 755 ./bin/spew $(PREFIX)/spew

.PHONY = build run install
