# Jim Tcl: a small-footprint implementation of the Tcl programming language.
# The implementation is Go; this Makefile is a convenience wrapper around the
# Go toolchain. See README for details.

GO ?= go

.PHONY: all jimsh test vet clean

all: jimsh

jimsh:
	$(GO) build -o jimsh .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

clean:
	rm -f jimsh
	$(GO) clean
