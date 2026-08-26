package main

import "testing"

func TestRunUnknownCommand(t *testing.T) {
	if err := run([]string{"bogus"}); err == nil {
		t.Fatal("run([\"bogus\"]): expected error for unknown command")
	}
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}} {
		if err := run(args); err != nil {
			t.Fatalf("run(%v): %v", args, err)
		}
	}
}

func TestRunVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"-v"}, {"--version"}} {
		if err := run(args); err != nil {
			t.Fatalf("run(%v): %v", args, err)
		}
	}
}
