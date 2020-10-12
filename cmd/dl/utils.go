package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
)

func recurseBelow(stopAtError bool, prefix string, act func(string) error) chan error {
	ret := make(chan error)

	var rec func(string) error
	rec = func(prefix string) error {
		var errAct error
		if err := act(prefix); err != nil {
			errAct = fmt.Errorf("%v: act: %v", prefix, err)
			if stopAtError {
				return errAct
			}
		}

		files, err := ioutil.ReadDir(prefix)
		if err != nil {
			return fmt.Errorf("%v: read dir: %v", prefix, err)
		}

		wg := sync.WaitGroup{}
		for _, f := range files {
			if !f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}

			recPrefix := filepath.Join(prefix, f.Name())

			wg.Add(1)
			go func() {
				if err := rec(recPrefix); err != nil {
					ret <- err
				}
				wg.Done()
			}()
		}
		wg.Wait()

		return errAct
	}

	go func() {
		if err := rec(prefix); err != nil {
			ret <- err
		}
		close(ret)
	}()

	return ret
}
