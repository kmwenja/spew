package spew

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"text/template"
	"time"
)

// Config holds values from the TOML config file
type Config struct {
	Template string   `toml:"template"`
	Sources  []Source `toml:"sources"`
}

// Source holds the configuration for a source
type Source struct {
	Name   string `toml:"name"`
	Type   string `toml:"type"`
	Script string `toml:"script"`
}

type output struct {
	Name  string
	Value string
}

// Spew takes a configuration and an io.Writer and
// writes its output to that io.Writer
func Spew(mainCtx context.Context, c Config, w io.Writer) error {
	template, err := template.New("spew").Parse(c.Template)
	if err != nil {
		return fmt.Errorf("could not parse template: %w", err)
	}

	templateCtx := make(map[string]string)
	bufw := bufio.NewWriter(w)

	render := func() error {
		err := template.Execute(bufw, templateCtx)
		if err != nil {
			return fmt.Errorf("could not render template: %w", err)
		}
		err = bufw.Flush()
		if err != nil {
			return fmt.Errorf("could not write to stdout: %w", err)
		}
		return nil
	}

	outChan := make(chan output)
	errChan := make(chan error)

	ctx, cancel := context.WithCancel(mainCtx)
	defer cancel()

	for _, s := range c.Sources {
		switch {
		case strings.Contains(s.Type, "timer"):
			go timerSource(ctx, s, outChan, errChan)
		case strings.Contains(s.Type, "listen"):
			go listenerSource(ctx, s, outChan, errChan)
		case strings.Contains(s.Type, "once"):
			o, err := runCommand(ctx, s.Script)
			if err != nil {
				return fmt.Errorf("could not run command(%s): %w: %q", s.Script, err, o)
			}

			templateCtx[strings.Title(s.Name)] = sanitize(o)
			err = render()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported source type: %s", s.Type)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-errChan:
			return e
		case o := <-outChan:
			templateCtx[strings.Title(o.Name)] = sanitize(o.Value)
			err := render()
			if err != nil {
				return err
			}
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

func timerSource(ctx context.Context, s Source, outChan chan<- output, errChan chan<- error) {
	parts := strings.Split(s.Type, ":")
	d, err := time.ParseDuration(parts[1])
	if err != nil {
		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("could not parse duration(%s): %w", parts[1], err):
		}
		return
	}

	o, err := runCommand(ctx, s.Script)
	if err != nil {
		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("could not run command(%s): %w: %q", s.Script, err, o):
		}
		return
	}

	select {
	case <-ctx.Done():
		return
	case outChan <- output{Name: s.Name, Value: o}:
	}

	ticker := time.NewTicker(d)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o, err := runCommand(ctx, s.Script)
			if err != nil {
				select {
				case <-ctx.Done():
				case errChan <- fmt.Errorf("could not run command(%s): %w: %q", s.Script, err, o):
				}
				return
			}

			select {
			case <-ctx.Done():
				return
			case outChan <- output{Name: s.Name, Value: o}:
			}
		}
	}
}

func listenerSource(ctx context.Context, s Source, outChan chan<- output, errChan chan<- error) {
	c := exec.CommandContext(ctx, "/bin/sh", "-c", s.Script)

	stdout, err := c.StdoutPipe()
	if err != nil {
		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("could not connect to command's stdout (%s): %w", s.Script, err):
		}
		return
	}

	stderr, err := c.StderrPipe()
	if err != nil {
		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("could not connect to command's stderr (%s): %w", s.Script, err):
		}
		return
	}

	err = c.Start()
	if err != nil {
		select {
		case <-ctx.Done():
		case errChan <- fmt.Errorf("could not start command (%s): %w", s.Script, err):
		}
		return
	}

	stdoutChan, stdoutErrChan := readLines(ctx, stdout)
	stderrChan, stderrErrChan := readLines(ctx, stderr)
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-stdoutErrChan:
			select {
			case <-ctx.Done():
			case errChan <- e:
			}
			return
		case e := <-stderrErrChan:
			select {
			case <-ctx.Done():
			case errChan <- e:
			}
			return
		case line := <-stdoutChan:
			select {
			case <-ctx.Done():
				return
			case outChan <- output{Name: s.Name, Value: line}:
			}
		case line := <-stderrChan:
			select {
			case <-ctx.Done():
			case outChan <- output{Name: s.Name, Value: line}:
			}
			return
		}
	}
}

func readLines(ctx context.Context, r io.Reader) (<-chan string, <-chan error) {
	outChan := make(chan string)
	errChan := make(chan error)
	go func() {
		defer close(outChan)
		defer close(errChan)
		bufr := bufio.NewReader(r)
		for {
			line, err := bufr.ReadString('\n')
			if err != nil {
				select {
				case <-ctx.Done():
				case errChan <- fmt.Errorf("could not read from reader: %w", err):
				}
				return
			}
			select {
			case <-ctx.Done():
				return
			case outChan <- line:
			}
		}
	}()
	return outChan, errChan
}
