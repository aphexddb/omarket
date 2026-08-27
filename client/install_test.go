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
	failIf    func(name string, args []string) bool
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if f.available[name] {
		return "/usr/bin/" + name, nil
	}
	return "", exec.ErrNotFound
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.ran = append(f.ran, append([]string{name}, args...))
	if f.failIf != nil && f.failIf(name, args) {
		return errors.New("boom")
	}
	return nil
}

func TestInstallPrefersOmarchyPkgAdd(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{
		"omarchy-pkg-add": true, "pkexec": true, "pacman": true, "sudo": true,
	}}

	msg, err := client.Install(r, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := []string{"pkexec", "/usr/bin/omarchy-pkg-add", "hello-shareware"}
	if len(r.ran) != 1 || !equalSlices(r.ran[0], want) {
		t.Fatalf("ran = %+v, want [%v]", r.ran, want)
	}
	if !strings.Contains(msg, "omarchy") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestInstallOmarchyWithoutPkexecUsesSudo(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{
		"omarchy-pkg-add": true, "sudo": true, "pacman": true,
	}}

	_, err := client.Install(r, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := []string{"sudo", "/usr/bin/omarchy-pkg-add", "hello-shareware"}
	if len(r.ran) != 1 || !equalSlices(r.ran[0], want) {
		t.Fatalf("ran = %+v, want [%v]", r.ran, want)
	}
}

func TestInstallFallsBackToPkexecPacman(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{"pacman": true, "pkexec": true, "yay": true}}

	msg, err := client.Install(r, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := []string{"pkexec", "/usr/bin/pacman", "-S", "--noconfirm", "--needed", "hello-shareware"}
	if len(r.ran) != 1 || !equalSlices(r.ran[0], want) {
		t.Fatalf("ran = %+v, want [%v]", r.ran, want)
	}
	if !strings.Contains(msg, "pacman") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestInstallFallsBackToSudoPacman(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{"pacman": true, "sudo": true}}

	_, err := client.Install(r, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := []string{"sudo", "/usr/bin/pacman", "-S", "--noconfirm", "--needed", "hello-shareware"}
	if len(r.ran) != 1 || !equalSlices(r.ran[0], want) {
		t.Fatalf("ran = %+v, want [%v]", r.ran, want)
	}
}

func TestInstallFallsBackToYay(t *testing.T) {
	r := &fakeRunner{available: map[string]bool{"yay": true}}

	msg, err := client.Install(r, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := []string{"/usr/bin/yay", "-S", "--noconfirm", "--needed", "hello-shareware"}
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
	if !strings.Contains(msg, "omarchy pkg add hello-shareware") {
		t.Fatalf("msg = %q, want it to contain the manual command", msg)
	}
}

func TestInstallOmarchyFailurePropagates(t *testing.T) {
	r := &fakeRunner{
		available: map[string]bool{"omarchy-pkg-add": true, "pkexec": true},
		failIf:    func(name string, _ []string) bool { return name == "pkexec" },
	}

	_, err := client.Install(r, "hello-shareware")
	if err == nil {
		t.Fatal("expected error when omarchy install fails")
	}
	if !strings.Contains(err.Error(), "couldn't install hello-shareware") {
		t.Fatalf("err = %q", err)
	}
}

func TestInstallMissingPackageIsClear(t *testing.T) {
	r := &errRunner{
		fakeRunner: &fakeRunner{available: map[string]bool{"omarchy-pkg-add": true, "pkexec": true}},
		err:        errors.New("error: target not found: hello-shareware"),
	}
	_, err := client.Install(r, "hello-shareware")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not in the omarchy package repo") {
		t.Fatalf("err = %q", err)
	}
}

func TestInstallOmarchyMissFallsBackToPacman(t *testing.T) {
	wrapped := &selectiveErrRunner{
		fakeRunner: &fakeRunner{available: map[string]bool{
			"omarchy-pkg-add": true, "pkexec": true, "pacman": true,
		}},
		firstErr: errors.New("error: target not found: hello-shareware"),
	}
	msg, err := client.Install(wrapped, "hello-shareware")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(msg, "pacman") {
		t.Fatalf("msg = %q, want pacman fallback", msg)
	}
	if len(wrapped.ran) != 2 {
		t.Fatalf("ran = %+v, want omarchy then pacman", wrapped.ran)
	}
	if got := wrapped.ran[1]; !equalSlices(got, []string{"pkexec", "/usr/bin/pacman", "-S", "--noconfirm", "--needed", "hello-shareware"}) {
		t.Fatalf("second command = %v", got)
	}
}

type selectiveErrRunner struct {
	*fakeRunner
	firstErr error
}

func (s *selectiveErrRunner) Run(name string, args ...string) error {
	n := len(s.ran)
	s.ran = append(s.ran, append([]string{name}, args...))
	if n == 0 {
		return s.firstErr
	}
	return nil
}

func TestInstallAuthCanceledIsClear(t *testing.T) {
	r := &errRunner{
		fakeRunner: &fakeRunner{available: map[string]bool{"omarchy-pkg-add": true, "pkexec": true}},
		err:        errors.New("Error executing command as another user: Request dismissed"),
	}
	_, err := client.Install(r, "hello-shareware")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "couldn't install hello-shareware: authentication canceled" {
		t.Fatalf("err = %q", err)
	}
}

type errRunner struct {
	*fakeRunner
	err error
}

func (e *errRunner) Run(name string, args ...string) error {
	e.ran = append(e.ran, append([]string{name}, args...))
	return e.err
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
