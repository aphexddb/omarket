.PHONY: demo
demo:
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) go run . ../testdata/hello-shareware.key
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) go run . ../testdata/tampered.key
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) go run . ../testdata/other-app.key
