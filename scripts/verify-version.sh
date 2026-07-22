#!/bin/sh

set -eu

version_file=${VERSION_FILE:-VERSION}
case $# in
	0)
		expected_version=
		source_sha=
		;;
	2)
		expected_version=$1
		source_sha=$2
		if [ -z "$expected_version" ] || [ -z "$source_sha" ]; then
			printf 'expected version and source SHA must both be non-empty\n' >&2
			exit 1
		fi
		;;
	*)
		printf 'usage: %s [expected-version full-source-sha]\n' "$0" >&2
		exit 1
		;;
esac

if [ ! -f "$version_file" ]; then
	printf 'version file not found: %s\n' "$version_file" >&2
	exit 1
fi

version=$(sed -n '1p' "$version_file")
line_count=$(wc -l < "$version_file" | tr -d '[:space:]')
byte_count=$(wc -c < "$version_file" | tr -d '[:space:]')
version_byte_count=$(printf '%s' "$version" | wc -c | tr -d '[:space:]')

if [ "$line_count" != "1" ] || [ "$byte_count" -ne "$((version_byte_count + 1))" ]; then
	printf '%s must contain exactly one version followed by one newline\n' "$version_file" >&2
	exit 1
fi

semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$'
if ! printf '%s\n' "$version" | grep -Eq "$semver"; then
	printf 'invalid SemVer in %s: %s\n' "$version_file" "$version" >&2
	exit 1
fi

if [ -n "$expected_version" ] && [ "$version" != "$expected_version" ]; then
	printf 'version mismatch: %s contains %s, build requested %s\n' "$version_file" "$version" "$expected_version" >&2
	exit 1
fi

if [ -n "$source_sha" ] && ! printf '%s\n' "$source_sha" | grep -Eq '^[0-9a-f]{40}$'; then
	printf 'source SHA must be exactly 40 lowercase hexadecimal characters\n' >&2
	exit 1
fi

printf '%s\n' "$version"
