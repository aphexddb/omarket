.PHONY: demo
demo:
	bundle check >/dev/null 2>&1 || bundle install
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) bundle exec ruby hello_shareware.rb ../testdata/hello-shareware.key
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) bundle exec ruby hello_shareware.rb ../testdata/tampered.key
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) bundle exec ruby hello_shareware.rb ../testdata/other-app.key
