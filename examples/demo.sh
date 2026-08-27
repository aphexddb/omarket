#!/bin/sh
# Run every language example against testdata and require identical reports.
set -eu

cd "$(dirname "$0")"

export SHAREWARE_PUBLIC_KEY=$(cat testdata/demo.pub)

user_gem_bin=$(ruby -e 'print File.join(Gem.user_dir, "bin")' 2>/dev/null) || user_gem_bin=
if [ -n "$user_gem_bin" ] && [ -d "$user_gem_bin" ]; then
	PATH="$user_gem_bin:$PATH"
	export PATH
fi

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

run_in() {
	dir=$1
	shift
	(cd "$dir" && "$@")
}

outdir=$(mktemp -d)
trap 'rm -rf "$outdir"' EXIT

# capture NAME CMD [ARGS...] — print the report and store it for comparison.
capture() {
	name=$1
	shift
	echo "--- $name ---"
	"$@" | tee "$outdir/$name"
}

echo "== C =="
make -C c hello-shareware
capture c-valid run_in c ./hello-shareware ../testdata/hello-shareware.key
capture c-tampered run_in c ./hello-shareware ../testdata/tampered.key
capture c-other run_in c ./hello-shareware ../testdata/other-app.key
capture c-missing run_in c ./hello-shareware /tmp/omarket-no-such.key

echo "== Go =="
capture go-valid run_in go go run . ../testdata/hello-shareware.key
capture go-tampered run_in go go run . ../testdata/tampered.key
capture go-other run_in go go run . ../testdata/other-app.key
capture go-missing run_in go go run . /tmp/omarket-no-such.key

echo "== Rust =="
capture rust-valid run_in rust cargo run --locked --quiet -- ../testdata/hello-shareware.key
capture rust-tampered run_in rust cargo run --locked --quiet -- ../testdata/tampered.key
capture rust-other run_in rust cargo run --locked --quiet -- ../testdata/other-app.key
capture rust-missing run_in rust cargo run --locked --quiet -- /tmp/omarket-no-such.key

echo "== Ruby =="
if ! command -v bundle >/dev/null 2>&1; then
	fail "bundle not found — install bundler, then: cd examples/ruby && bundle install"
fi
(cd ruby && bundle check >/dev/null 2>&1 || bundle install)
capture ruby-valid run_in ruby bundle exec ruby hello_shareware.rb ../testdata/hello-shareware.key
capture ruby-tampered run_in ruby bundle exec ruby hello_shareware.rb ../testdata/tampered.key
capture ruby-other run_in ruby bundle exec ruby hello_shareware.rb ../testdata/other-app.key
capture ruby-missing run_in ruby bundle exec ruby hello_shareware.rb /tmp/omarket-no-such.key

echo "== compare reports =="
grep -q '\[x\] registered' "$outdir/c-valid" || fail "valid key did not register"
grep -q 'bad signature' "$outdir/c-tampered" || fail "tampered key was not rejected"
grep -q 'some-other-app' "$outdir/c-other" || fail "wrong-app key was not rejected"
grep -q 'no license file' "$outdir/c-missing" || fail "missing key was not rejected"

for case in valid tampered other missing; do
	for lang in go rust ruby; do
		if ! diff -u "$outdir/c-$case" "$outdir/$lang-$case"; then
			fail "$lang $case report differs from C"
		fi
		echo "ok $lang $case"
	done
done

echo "OK: C, Go, Rust, and Ruby ran testdata and printed identical reports"
