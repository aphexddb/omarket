.PHONY: demo
demo:
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) cargo run --locked --quiet -- ../testdata/hello-shareware.key
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) cargo run --locked --quiet -- ../testdata/tampered.key
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) cargo run --locked --quiet -- ../testdata/other-app.key
