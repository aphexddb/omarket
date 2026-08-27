CC      ?= cc
CFLAGS  ?= -O2 -Wall -Wextra -std=c11
LDLIBS  := -lcrypto

hello-shareware: hello_shareware.c
	$(CC) $(CFLAGS) -o $@ $< $(LDLIBS)

# Run against the demo fixtures instead of a real purchase.
.PHONY: demo
demo: hello-shareware
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) ./hello-shareware ../testdata/hello-shareware.key
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) ./hello-shareware ../testdata/tampered.key
	SHAREWARE_PUBLIC_KEY=$$(cat ../testdata/demo.pub) ./hello-shareware ../testdata/other-app.key

.PHONY: clean
clean:
	rm -f hello-shareware
