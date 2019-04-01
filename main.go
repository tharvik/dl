package main

import (
	"errors"
	"flag"
	"os"
)
import "github.com/tharvik/dl/lib"

func add(args []string) error {
	flags := flag.NewFlagSet("add", flag.ContinueOnError)
	output := flags.String("o", "", "output filename")
	fetcherName := flags.String("f", "", "fetcher name")

	err := flags.Parse(args)
	if err != nil {
		return err
	}

	if *output == "" {
		return errors.New("no output specified")
	}

	fetcher, err := lib.GetFetcher(*fetcherName)
	if err != nil {
		return err
	}

	dl := lib.Download{*output, fetcher, flags.Args()}

	return lib.AddDownload(dl)
}

func fetcher(args []string) error {
	if len(args) < 2 {
		return errors.New("fetcher need <name> and <args..>")
	}

	fetcher := lib.Fetcher{args[0], args[1:]}
	return lib.AddFetcher(fetcher)
}

func parse(args []string) error {
	return errors.New("TODO")
}

func fetch(args []string) error {
	return errors.New("TODO")
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
