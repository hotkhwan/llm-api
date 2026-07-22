#!/bin/sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
validator="$script_dir/verify-version.sh"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM
version_file="$tmp_dir/VERSION"
full_sha=0123456789abcdef0123456789abcdef01234567

expect_success() {
	content=$1
	expected=$2
	printf '%s' "$content" > "$version_file"
	VERSION_FILE="$version_file" "$validator" "$expected" "$full_sha" >/dev/null
}

expect_failure() {
	content=$1
	expected=${2-0.1.0}
	sha=${3-$full_sha}
	printf '%s' "$content" > "$version_file"
	if VERSION_FILE="$version_file" "$validator" "$expected" "$sha" >/dev/null 2>&1; then
		printf 'expected validation failure for content %s\n' "$content" >&2
		exit 1
	fi
}

expect_success '0.1.0
' '0.1.0'
expect_success '1.2.3-rc.1+build.5
' '1.2.3-rc.1+build.5'

expect_failure '0.1.0' # missing trailing newline
expect_failure '0.1.0

'                    # extra line
expect_failure '01.2.3
' '01.2.3'           # leading zero
expect_failure '1.2.3-01
' '1.2.3-01'         # numeric pre-release leading zero
expect_failure '0.1.0
' '0.1.1'            # build/file mismatch
expect_failure '0.1.0
' '0.1.0' '8ae3c8b'  # abbreviated SHA
expect_failure '0.1.0
' '' "$full_sha"     # missing build version
expect_failure '0.1.0
' '0.1.0' ''          # missing build SHA

printf '0.1.0\n' > "$version_file"
if VERSION_FILE="$version_file" "$validator" '0.1.0' >/dev/null 2>&1; then
	printf 'expected validation failure when only one build value is supplied\n' >&2
	exit 1
fi

printf 'version contract tests passed\n'
