all: build

build:
	go build -o spew .

run: build
	./spew test.toml

.PHONY = build
