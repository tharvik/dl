package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const TestDir = "testdata"

func getEnvForTest() []string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	env := os.Environ()
	for i, line := range env {
		if strings.HasPrefix(line, "PATH=") {
			env[i] = "PATH=" + cwd + ":" + line[5:]
			return env
		}
	}

	return append(env, "PATH="+cwd)
}

func TestExec(t *testing.T) {
	tests, err := ioutil.ReadDir(TestDir)
	if err != nil {
		panic(err)
	}

	testEnv := getEnvForTest()

	for _, test := range tests {
		if test.Mode()&0100 == 0 || test.IsDir() {
			continue
		}

		t.Run(test.Name(), func(t *testing.T) {
			//t.Parallel()

			cmd := exec.Command("./" + test.Name())
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stdout
			cmd.Env = testEnv
			cmd.Dir = TestDir

			err := cmd.Run()
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
