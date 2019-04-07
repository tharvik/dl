package main

import (
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
)
import "github.com/tharvik/dl/lib"

func errAppend(prefix string, err error) error {
	if err == nil {
		return nil
	}
	return errors.New(prefix + ": " + err.Error())
}

func add(args []string) error {
	flags := flag.NewFlagSet("add", flag.ContinueOnError)
	output := flags.String("o", "", "output filename")
	fetcherName := flags.String("f", "", "fetcher name")

	err := flags.Parse(args)
	if err != nil {
		return errAppend("add", err)
	}

	if *output == "" {
		return errors.New("no output specified")
	}

	fetcher, err := lib.GetFetcher(*fetcherName)
	if err != nil {
		return errAppend("add", err)
	}

	dl := lib.Download{*output, fetcher, flags.Args()}

	return errAppend("add", lib.AddDownload(dl))
}

func fetcher(args []string) error {
	if len(args) < 2 {
		return errors.New("fetcher: need <name> and <args..>")
	}

	fetcher := lib.Fetcher{args[0], args[1:]}
	return errAppend("fetcher", lib.AddFetcher(fetcher))
}

func drainInto(d chan error, chans []chan error) {
	cases := make([]reflect.SelectCase, len(chans))
	for i, ch := range chans {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}

	for len(cases) > 0 {
		chosen, value, ok := reflect.Select(cases)
		if ok {
			d <- value.Interface().(error)
		} else {
			cases = append(cases[:chosen], cases[chosen+1:]...)
		}
	}

	close(d)
}

func recurseBelow(prefix string, act func(string) error) chan error {
	ret := make(chan error)

	var rec func(chan error, string)
	rec = func(ret chan error, prefix string) {
		err := act(prefix)
		if err != nil {
			ret <- err
			close(ret)
			return
		}

		files, err := ioutil.ReadDir(prefix)
		if err != nil {
			ret <- errAppend(prefix, err)
			close(ret)
			return
		}

		rets := make([]chan error, 0)
		for _, f := range files {
			if !f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}

			recRet := make(chan error)
			recPrefix := filepath.Join(prefix, f.Name())
			go rec(recRet, recPrefix)

			rets = append(rets, recRet)
		}

		drainInto(ret, rets)
	}
	go rec(ret, prefix)

	return ret
}

func parse(args []string) error {
	if len(args) == 0 {
		args = []string{"."}
	}

	rets := make([]chan error, len(args))
	for i, arg := range args {
		rets[i] = recurseBelow(arg, func(prefix string) error {
			script := filepath.Join(prefix, lib.ScriptFile)
			// TODO add saved state
			cmd := exec.Command("./" + script)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Run()
			if err != nil {
				switch v := err.(type) {
				case *os.PathError:
					code := v.Err.(syscall.Errno)
					// "no such file or directory"
					if code != 0x02 {
						return errAppend(prefix, err)
					}
				default:
					return errAppend(prefix, err)
				}
			}

			return nil
		})
	}
	ret := make(chan error, 1)
	go drainInto(ret, rets)

	for err := range ret {
		return errAppend("parse", err)
	}

	return nil
}

func fetch(args []string) error {
	if len(args) == 0 {
		args = []string{"."}
	}

	return errors.New("fetch: TODO")
}

func main() {
	jumpTable := map[string]func([]string) error{
		"add":     add,
		"parse":   parse,
		"fetch":   fetch,
		"fetcher": fetcher,
	}

	// TODO use context
	err := lib.Init()
	if err != nil {
		panic(err)
	}

	if len(os.Args) == 1 {
		err = parse([]string{})
		if err == nil {
			err = fetch([]string{})
		}
	} else {
		act, ok := jumpTable[os.Args[1]]

		if ok {
			err = act(os.Args[2:])
		} else {
			err = errors.New("unknown command")
		}
	}

	if err != nil {
		panic(err)
	}
}
