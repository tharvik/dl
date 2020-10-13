package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const TestDir = "testdata"

func TestExec(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	println("testdata:", filepath.Join(cwd, TestDir))
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
