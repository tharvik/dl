package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)
import "github.com/tharvik/dl/lib"

type JobServer chan int

func errPrefix(err error, prefixes ...string) error {
	if err == nil {
		return nil
	}
	prefixes = append(prefixes, err.Error())
	return errors.New(strings.Join(prefixes, ": "))
}

func add(_ *log.Logger, args []string) error {
	flags := flag.NewFlagSet("add", flag.ContinueOnError)
	output := flags.String("o", "", "output filename")
	fetcherName := flags.String("f", "", "fetcher name")

	if err := flags.Parse(args); err != nil {
		return errPrefix(err, "add")
	}

	if *output == "" {
		return errors.New("add: no output specified")
	}

	if *fetcherName == "" {
		return errors.New("add: no fetcher specified")
	}

	fmt.Println("++", *output)

	db, err := lib.NewDB(".")
	if err != nil {
		return errPrefix(err, "add")
	}

	fetcher, err := db.GetFetcher(*fetcherName)
	if err != nil {
		return errPrefix(err, "add")
	}

	dl := lib.Download{
		Name:      *output,
		Fetcher:   fetcher,
		Arguments: flags.Args(),
	}
	return errPrefix(db.AddDownload(dl), "add")
}

func fetcher(_ *log.Logger, args []string) error {
	if len(args) < 2 {
		return errors.New("fetcher: need <name> and <args..>")
	}

	db, err := lib.NewDB(".")
	if err != nil {
		return errPrefix(err, "fetcher")
	}

	fetcher := lib.Fetcher{
		Name:      args[0],
		Arguments: args[1:],
	}
	return errPrefix(db.AddFetcher(fetcher), "fetcher")
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
}

// act:	return errs on chan; if !ret, stop process
func recurseBelow(prefix string, act func(chan error, string) bool) chan error {
	ret := make(chan error)

	var rec func(chan error, string)
	rec = func(ret chan error, prefix string) {
		cont := act(ret, prefix)
		if !cont {
			close(ret)
			return
		}

		files, err := ioutil.ReadDir(prefix)
		if err != nil {
			ret <- errPrefix(err, prefix)
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
		close(ret)
	}
	go rec(ret, prefix)

	return ret
}

func parse(logger *log.Logger, args []string) error {
	flags := flag.NewFlagSet("parse", flag.ContinueOnError)
	jobs := flags.Int("j", runtime.NumCPU(), "jobs")

	if err := flags.Parse(args); err != nil {
		return errPrefix(err, "parse")
	}

	js := make(JobServer, *jobs)
	ret := recurseBelow(".", func(ret chan error, prefix string) bool {
		logger.Printf("parse: recurse %s: entry", prefix)

		_, err := os.Stat(filepath.Join(prefix, lib.ScriptFile))
		if err != nil {
			if os.IsNotExist(err) {
				logger.Printf("parse: recurse %s: missing script", prefix)
				return true
			}
			ret <- errPrefix(err, prefix)
			return false
		}

		db, err := lib.NewDB(prefix)
		if err != nil {
			ret <- errPrefix(err, prefix)
			return false
		}

		args, err := db.GetState()
		if err != nil {
			ret <- errPrefix(err, prefix)
			return false
		}

		token := <-js
		defer func() { js <- token }()

		fmt.Println("~~", prefix)

		cmd := exec.Command("./"+lib.ScriptFile, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Dir = prefix
		if err := cmd.Run(); err != nil {
			ret <- errPrefix(err, prefix)
			return false
		}

		return true
	})

	for i := 0; i < *jobs; i++ {
		js <- i
	}

	var firsterr error = nil
	for err := range ret {
		if firsterr == nil {
			firsterr = errPrefix(err, "parse")
		}
	}

	return firsterr
}

func fetch(logger *log.Logger, args []string) error {
	flags := flag.NewFlagSet("fetch", flag.ContinueOnError)
	jobs := flags.Int("j", runtime.NumCPU(), "jobs")

	if err := flags.Parse(args); err != nil {
		return errPrefix(err, "fetch")
	}

	js := make(JobServer, *jobs)
	ret := recurseBelow(".", func(ret chan error, prefix string) bool {
		_, err := os.Stat(filepath.Join(prefix, lib.ScriptFile))
		if err != nil {
			if os.IsNotExist(err) {
				return true
			}
			ret <- errPrefix(err, prefix)
			return false
		}

		db, err := lib.NewDB(prefix)
		if err != nil {
			ret <- errPrefix(err, prefix)
			return false
		}

		dls, err := db.GetDownloads()
		if err != nil {
			ret <- errPrefix(err, prefix)
			return false
		}

		rets := make([]chan error, len(dls))
		for i, dl := range dls {
			dlRet := make(chan error)
			rets[i] = dlRet
			go func(dl lib.Download) {
				defer close(dlRet)

				token := <-js
				defer func() { js <- token }()

				fmt.Println(">>", dl.Name)

				output, err := os.Create(filepath.Join(prefix, dl.Name))
				if err != nil {
					dlRet <- errPrefix(err, prefix, dl.Name)
					return
				}

				cmdArgs := make([]string, 0, len(dl.Fetcher.Arguments)+len(dl.Arguments))
				cmdArgs = append(cmdArgs, dl.Fetcher.Arguments...)
				cmdArgs = append(cmdArgs, dl.Arguments...)

				cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
				cmd.Stdout = output
				cmd.Stderr = os.Stderr
				cmd.Dir = prefix
				logger.Printf("fetch: recurse %s: running \"%s\"", prefix, strings.Join(cmdArgs, " "))
				err = cmd.Run()
				if err != nil {
					dlRet <- errPrefix(err, prefix, dl.Name)
					return
				}

				err = db.DelDownload(dl)
				if err != nil {
					dlRet <- errPrefix(err, prefix, dl.Name)
					return
				}
			}(dl)
		}

		drainInto(ret, rets)
		return true
	})

	for i := 0; i < *jobs; i++ {
		js <- i
	}

	var reterr error = nil
	for err := range ret {
		if reterr == nil {
			reterr = errPrefix(err, "fetch")
		}
	}

	return reterr
}

func save(_ *log.Logger, args []string) error {
	db, err := lib.NewDB(".")
	if err != nil {
		return errPrefix(err, "save")
	}
	return errPrefix(db.SetState(args), "save")
}

type NopWriter struct{}

func (NopWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func main() {
	jumpTable := map[string]func(*log.Logger, []string) error{
		"add":     add,
		"parse":   parse,
		"fetch":   fetch,
		"fetcher": fetcher,
		"save":    save,
	}

	flags := flag.NewFlagSet("", flag.ContinueOnError)
	verbose := flags.Bool("v", false, "verbose")

	prefix := fmt.Sprintf("[%v %v] ", os.Args[0], os.Getpid())

	var err error
	if err = flags.Parse(os.Args[1:]); err == nil {
		var logger *log.Logger
		if *verbose {
			logger = log.New(os.Stderr, prefix, log.Ltime|log.Lshortfile)
		} else {
			logger = log.New(new(NopWriter), "", 0)
		}

		if len(flags.Args()) == 0 {
			err = parse(logger, []string{})
			if err == nil {
				err = fetch(logger, []string{})
			}
		} else {
			if act, ok := jumpTable[flags.Args()[0]]; ok {
				err = act(logger, flags.Args()[1:])
			} else {
				err = errors.New("unknown command")
			}
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%v%v\n", prefix, err)
		os.Exit(1)
	}
}
