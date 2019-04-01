package test

import (
	"io/ioutil"
	"os"
	"os/exec"
	"testing"
)

func TestShell(t *testing.T) {
	tests, err := ioutil.ReadDir(".")
	if err != nil {
		panic(err)
	}

	for _, test := range tests {
		if test.Mode()&0100 == 0 {
			continue
		}

		t.Run(test.Name(), func(tt *testing.T) {
			cmd := exec.Command("./" + test.Name())
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stdout

			err := cmd.Run()
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
