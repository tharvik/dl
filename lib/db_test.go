package lib

import "testing"

func TestPackaging(t *testing.T) {
	pipe := func(input []string) {
		output, err := unpackArguments(packArguments(input))
		if err != nil {
			panic(err)
		}
		if len(input) != len(output) {
			panic("not same size")
		}
		for i := range input {
			if input[i] != output[i] {
				panic("not same elem")
			}
		}
	}

	for _, elem := range []struct {
		name string
		args []string
	}{
		{"empty", []string{}},
		{"single", []string{"abc"}},
		{"multi", []string{"a", "bb", "ccc"}},
	} {
		t.Run(elem.name, func(*testing.T) { pipe(elem.args) })
	}
}
