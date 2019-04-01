set -eu
set -o pipefail

tmpdir=$(mktemp --directory --tmpdir=.)
cd "$tmpdir"

cleanup() {
	rm -r "../$tmpdir"
}
trap cleanup EXIT

fail() {
	echo "$@"
	exit 1
}

skip() {
	echo "Skipped because " "$@"
	exit 0
}

create_script() {
	local inner
	inner() {
		local dir="$1"
		local script="$dir/.dl"

		cat > "$script" <<-EOF
		#!/bin/sh -eu
		$(cat)
		EOF
		chmod +x "$script"
	}

	if [ $# -eq 0 ]
	then
		cat | inner .
	else
		local dir content
		content="$(cat)"
		for dir
		do
			mkdir -p "$dir"
			echo "$content" | inner "$dir"
		done
	fi
}

lsdir() {
	ls -a | grep -vE '^\.\.?$'
}
