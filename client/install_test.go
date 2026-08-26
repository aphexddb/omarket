package client_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
)

type fakeRunner struct {
	available map[string]bool
	ran       [][]string
	failOn    string
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if f.available[name] {
		return "/usr/bin/" + name, nil
	}
	return "", exec.ErrNotFound
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.ran = append(f.ran, append([]string{name}, args...))
	if name == f.failOn {
		return errors.New("boom")
	}
	return nil
}

func TestInstallPrefersPacman(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{"pacman": true, "yay": true}}

	msg, err := client.Install(r, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(r.ran) != 1 {
		t.Fatalf("ran = %+v, want exactly one command", r.ran)
	}
	want := []string{"sudo", "pacman", "-S", "--noconfirm", "hello-shareware"}
	if !equalSlices(r.ran[0], want) {
		t.Fatalf("ran[0] = %v, want %v", r.ran[0], want)
	}
	if !strings.Contains(msg, "pacman") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestInstallFallsBackToYay(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{"yay": true}}

	msg, err := client.Install(r, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := []string{"yay", "-S", "--noconfirm", "hello-shareware"}
	if len(r.ran) != 1 || !equalSlices(r.ran[0], want) {
		t.Fatalf("ran = %+v, want [%v]", r.ran, want)
	}
	if !strings.Contains(msg, "yay") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestInstallNeitherAvailable(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{}}

	msg, err := client.Install(r, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(r.ran) != 0 {
		t.Fatalf("ran = %+v, want no exec calls", r.ran)
	}
	if !strings.Contains(msg, "sudo pacman -S --noconfirm hello-shareware") {
		t.Fatalf("msg = %q, want it to contain the manual command", msg)
	}
}

func TestInstallPacmanFailurePropagates(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{"pacman": true}, failOn: "sudo"}

	_, err := client.Install(r, "hello-shareware")
	if err == nil {
		t.Fatal("expected error when pacman install fails")
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
