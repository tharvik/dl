package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const TestDir = "testdata"
const LongTestSuffix = "_long"

func TestExec(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	tests, err := ioutil.ReadDir(filepath.Join(cwd, TestDir))
	if err != nil {
		panic(err)
	}

	for _, test := range tests {
		if test.Mode()&0100 == 0 || test.IsDir() {
			continue
		}

		testName := test.Name()
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			if strings.HasSuffix(testName, LongTestSuffix) && testing.Short() {
				t.Skip("short enabled")
			}

			cmd := exec.Command("./" + testName)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Dir = filepath.Join(cwd, TestDir)

			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
