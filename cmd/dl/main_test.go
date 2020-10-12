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

func getEnvForTest(cwd string) []string {
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

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	testEnv := getEnvForTest(cwd)

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
			cmd.Env = testEnv
			cmd.Dir = filepath.Join(cwd, TestDir)

			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
