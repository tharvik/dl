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
	"syscall"

	"github.com/tharvik/dl/internal"
)

type JobServer chan struct{}

func add(_ *log.Logger, args []string) error {
	flags := flag.NewFlagSet("add", flag.ContinueOnError)
	output := flags.String("o", "", "output filename")
	fetcherName := flags.String("f", "", "fetcher name")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("flags parse: %v", err)
	}

	if *output == "" {
		return errors.New("no output specified")
	}

	if *fetcherName == "" {
		return errors.New("no fetcher specified")
	}

	fmt.Println("++", *output)

	db, err := internal.NewDB(".")
	if err != nil {
		return fmt.Errorf("new db: %v", err)
	}

	fetcher, err := db.GetFetcher(*fetcherName)
	if err != nil {
		return fmt.Errorf("get fetcher: %v", err)
	}

	dl := internal.Download{
		Name:      *output,
		Fetcher:   fetcher,
		Arguments: flags.Args(),
	}
	if err := db.AddDownload(dl); err != nil {
		return fmt.Errorf("add download: %v", err)
	}

	return nil
}

func fetcher(_ *log.Logger, args []string) error {
	if len(args) < 2 {
		return errors.New("need <name> and <args..>")
	}

	db, err := internal.NewDB(".")
	if err != nil {
		return fmt.Errorf("new db: %v", err)
	}

	fetcher := internal.Fetcher{
		Name:      args[0],
		Arguments: args[1:],
	}
	if err := db.AddFetcher(fetcher); err != nil {
		return fmt.Errorf("add fetcher: %v", err)
	}

	return nil
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
			ret <- fmt.Errorf("%v: %v", prefix, err)
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
		return fmt.Errorf("flags parse: %v", err)
	}

	js := make(JobServer, *jobs)
	ret := recurseBelow(".", func(ret chan error, prefix string) bool {
		logger.Printf("parse: recurse %s: entry", prefix)

		_, err := os.Stat(filepath.Join(prefix, internal.ScriptFile))
		if err != nil {
			if os.IsNotExist(err) {
				logger.Printf("parse: recurse %s: missing script", prefix)
				return true
			}
			ret <- fmt.Errorf("%v: %v", prefix, err)
			return false
		}

		db, err := internal.NewDB(prefix)
		if err != nil {
			ret <- fmt.Errorf("%v: %v", prefix, err)
			return false
		}

		args, err := db.GetState()
		if err != nil {
			ret <- fmt.Errorf("%v: %v", prefix, err)
			return false
		}

		token := <-js
		defer func() { js <- token }()

		fmt.Println("~~", prefix)

		cmd := exec.Command("./"+internal.ScriptFile, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Dir = prefix
		if err := cmd.Run(); err != nil {
			ret <- fmt.Errorf("%v: %v", prefix, err)
			return false
		}

		return true
	})

	for i := 0; i < *jobs; i++ {
		js <- struct{}{}
	}

	var firsterr error
	for err := range ret {
		if firsterr == nil {
			firsterr = err
		}
	}

	return firsterr
}

func fetch(logger *log.Logger, args []string) error {
	flags := flag.NewFlagSet("fetch", flag.ContinueOnError)
	jobs := flags.Int("j", runtime.NumCPU(), "jobs")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("flags parse: %v", err)
	}

	js := make(JobServer, *jobs)
	ret := recurseBelow(".", func(ret chan error, prefix string) bool {
		_, err := os.Stat(filepath.Join(prefix, internal.ScriptFile))
		if err != nil {
			if os.IsNotExist(err) {
				return true
			}
			ret <- fmt.Errorf("%v: %v", prefix, err)
			return false
		}

		db, err := internal.NewDB(prefix)
		if err != nil {
			ret <- fmt.Errorf("%v: %v", prefix, err)
			return false
		}

		dls, err := db.GetDownloads()
		if err != nil {
			ret <- fmt.Errorf("%v: %v", prefix, err)
			return false
		}

		rets := make([]chan error, len(dls))
		for i, dl := range dls {
			dlRet := make(chan error)
			rets[i] = dlRet
			go func(dl internal.Download) {
				defer close(dlRet)

				token := <-js
				defer func() { js <- token }()

				fmt.Println(">>", dl.Name)

				outputPath := filepath.Join(prefix, dl.Name)

				err := os.MkdirAll(filepath.Dir(outputPath), os.ModePerm)
				if err != nil {
					dlRet <- fmt.Errorf("%v: %v: %v", prefix, err, dl.Name)
					return
				}

				output, err := os.Create(outputPath)
				if err != nil {
					dlRet <- fmt.Errorf("%v: %v: %v", prefix, err, dl.Name)
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
					dlRet <- fmt.Errorf("%v: %v: %v", prefix, err, dl.Name)
					return
				}

				err = db.DelDownload(dl)
				if err != nil {
					dlRet <- fmt.Errorf("%v: %v: %v", prefix, err, dl.Name)
					return
				}
			}(dl)
		}

		drainInto(ret, rets)
		return true
	})

	for i := 0; i < *jobs; i++ {
		js <- struct{}{}
	}

	var reterr error = nil
	for err := range ret {
		if reterr == nil {
			reterr = err
		}
	}

	return reterr
}

func save(_ *log.Logger, args []string) error {
	db, err := internal.NewDB(".")
	if err != nil {
		return fmt.Errorf("new db: %v", err)
	}

	if err := db.SetState(args); err != nil {
		return fmt.Errorf("set state: %v", err)
	}

	return nil
}

func gen(logger *log.Logger, args []string) error {
	self_name := filepath.Base(os.Args[0])
	script := "#!/bin/sh\n\nexec " + self_name + " " + strings.Join(args, " ") + " \"$@\""
	return ioutil.WriteFile(".dl", []byte(script), 0755)
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
		"gen":     gen,
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
			sub_name := flags.Args()[0]
			sub_args := flags.Args()[1:]
			if act, ok := jumpTable[sub_name]; ok {
				if err = act(logger, sub_args); err != nil {
					err = fmt.Errorf("%v: %v", sub_name, err)
				}
			} else {
				sub_name := filepath.Base(os.Args[0]) + "-" + sub_name
				var sub_exec string
				sub_exec, err = exec.LookPath(sub_name)
				if err != nil {
					err = fmt.Errorf("whereis '%v' : %v", sub_name, err)
				} else {
					sub_args_with_argv0 := []string{sub_exec}
					sub_args_with_argv0 = append(sub_args_with_argv0, sub_args...)
					if err = syscall.Exec(sub_exec, sub_args_with_argv0, os.Environ()); err != nil {
						err = fmt.Errorf("exec '%v': %v", sub_name, err)
					}
				}
			}
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%v%v\n", prefix, err)
		os.Exit(1)
	}
}
