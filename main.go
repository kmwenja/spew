package main

import (
	"bufio"
	"context"
	"fmt"
	"html/template"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Source holds the configuration for a source
type Source struct {
	Name   string `toml:"name"`
	Type   string `toml:"type"`
	Script string `toml:"script"`
}

// Output holds the output for a source
type Output struct {
	Name  string
	Value string
}

// Config holds values from the TOML config file
type Config struct {
	Template string   `toml:"template"`
	Sources  []Source `toml:"sources"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: spew <config-file>")
		os.Exit(1)
	}

	var c Config
	_, err := toml.DecodeFile(os.Args[1], &c)
	if err != nil {
		panic(fmt.Errorf("could not read from config file: %w", err))
	}

	template, err := template.New("spew").Parse(c.Template)
	if err != nil {
		panic(fmt.Errorf("could not parse template: %w", err))
	}

	templateCtx := make(map[string]string)
	stdout := bufio.NewWriter(os.Stdout)

	render := func() {
		err := template.Execute(stdout, templateCtx)
		if err != nil {
			panic(fmt.Errorf("could not render template: %w", err))
		}
		err = stdout.Flush()
		if err != nil {
			panic(fmt.Errorf("could not write to stdout: %w", err))
		}
	}

	outChan := make(chan Output)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, s := range c.Sources {
		switch {
		case strings.Contains(s.Type, "timer"):
			go timerSource(ctx, s, outChan)
		case strings.Contains(s.Type, "listen"):
			go listenerSource(ctx, s, outChan)
		case strings.Contains(s.Type, "once"):
			o, err := runCommand(ctx, s.Script)
			if err != nil {
				panic(fmt.Errorf("could not run command(%s): %w: %q", s.Script, err, o))
			}

			templateCtx[strings.Title(s.Name)] = sanitize(o)
			render()
		default:
			panic(fmt.Errorf("unsupported source type: %s", s.Type))
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case o := <-outChan:
			templateCtx[strings.Title(o.Name)] = sanitize(o.Value)
			render()
		}
	}
}

func sanitize(s string) string {
	res := s
	res = strings.ReplaceAll(res, "\n", "")
	res = strings.ReplaceAll(res, "\r", "")
	return res
}

func runCommand(ctx context.Context, cmd string) (string, error) {
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	outBytes, err := c.CombinedOutput()
	return string(outBytes), err
}

func timerSource(ctx context.Context, s Source, outChan chan<- Output) {
	parts := strings.Split(s.Type, ":")
	d, err := time.ParseDuration(parts[1])
	if err != nil {
		panic(fmt.Errorf("could not parse duration(%s): %w", parts[1], err))
	}

	o, err := runCommand(ctx, s.Script)
	if err != nil {
		panic(fmt.Errorf("could not run command(%s): %w: %q", s.Script, err, o))
	}
	select {
	case <-ctx.Done():
		return
	case outChan <- Output{Name: s.Name, Value: o}:
	}

	ticker := time.NewTicker(d)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o, err := runCommand(ctx, s.Script)
			if err != nil {
				panic(fmt.Errorf("could not run command(%s): %w: %q", s.Script, err, o))
			}
			select {
			case <-ctx.Done():
				return
			case outChan <- Output{Name: s.Name, Value: o}:
			}
		}
	}
}

func listenerSource(ctx context.Context, s Source, outChan chan<- Output) {
	c := exec.CommandContext(ctx, "/bin/sh", "-c", s.Script)
	stdout, err := c.StdoutPipe()
	if err != nil {
		panic(fmt.Errorf("could not connect to command's stdout (%s): %w", s.Script, err))
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		panic(fmt.Errorf("could not connect to command's stderr (%s): %w", s.Script, err))
	}
	err = c.Start()
	if err != nil {
		panic(fmt.Errorf("could not start command (%s): %w", s.Script, err))
	}
	stdoutChan := readLines(ctx, stdout)
	stderrChan := readLines(ctx, stderr)
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-stdoutChan:
			select {
			case <-ctx.Done():
				return
			case outChan <- Output{Name: s.Name, Value: line}:
			}
		case line := <-stderrChan:
			panic(fmt.Errorf("stderr detected when running command (%s): %q", s.Script, line))
		}
	}
}

func readLines(ctx context.Context, r io.Reader) <-chan string {
	outChan := make(chan string)
	go func() {
		bufr := bufio.NewReader(r)
		for {
			line, err := bufr.ReadString('\n')
			if err != nil {
				panic(fmt.Errorf("could not read from reader: %w", err))
			}
			select {
			case <-ctx.Done():
				close(outChan)
				return
			case outChan <- line:
			}
		}
	}()
	return outChan
}
