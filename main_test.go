package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const TestDir = "test"

func getEnvForTest() []string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	env := os.Environ()
	for i, line := range env {
		if strings.HasPrefix(line, "PATH=") {
			env[i] = line + ":" + cwd
			return env
		}
	}

	return append(env, "PATH="+cwd)
}

func TestShell(t *testing.T) {
	tests, err := ioutil.ReadDir(TestDir)
	if err != nil {
		panic(err)
	}

	testEnv := getEnvForTest()

	for _, test := range tests {
		if test.Mode()&0100 == 0 || test.IsDir() {
			continue
		}

		t.Run(test.Name(), func(tt *testing.T) {
			cmd := exec.Command("./" + test.Name())
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stdout
			cmd.Env = testEnv
			cmd.Dir = TestDir

			err := cmd.Run()
			if err != nil {
				tt.Fatal(err)
			}
		})
	}
}
