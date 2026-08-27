package client

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner abstracts process execution so install logic is testable
// without a real pacman/yay/sudo on the machine.
type CommandRunner interface {
	// Run executes name with args. Stdin is not attached: privileged
	// helpers (pkexec/sudo) must use Omarchy's polkit dialog rather than
	// stealing the TUI's terminal for a password prompt.
	Run(name string, args ...string) error
	// LookPath reports whether name is found in PATH. The returned path
	// is absolute when the implementation uses os/exec.LookPath.
	LookPath(name string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	// Leave Stdin nil so sudo/pkexec cannot read a password from the TUI
	// tty. pkexec talks to the Omarchy polkit agent instead.
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, compactOutput(msg))
}

func (execRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// DefaultRunner shells out for real; swap in tests via Install's runner arg.
var DefaultRunner CommandRunner = execRunner{}

func compactOutput(s string) string {
	lines := strings.Split(s, "\n")
	var last string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			last = line
		}
	}
	if last == "" {
		return s
	}
	return last
}

// Install installs pkgname. Order:
//  1. omarchy pkg add — Arch + the Omarchy package repo, with Omarchy's
//     polkit GUI for sudo when elevation is needed.
//  2. pkexec pacman -S --noconfirm --needed — same GUI, on machines
//     without the omarchy helper.
//  3. yay, if neither omarchy nor pacman is on PATH.
//
// A nil runner uses DefaultRunner. Missing tools are not an error: the
// returned message tells the user what to run by hand.
func Install(runner CommandRunner, pkgname string) (string, error) {
	if runner == nil {
		runner = DefaultRunner
	}

	if HasHelper(runner, "omarchy-pkg-add") || HasHelper(runner, "omarchy") {
		msg, err := InstallOnce(runner, pkgname, "omarchy")
		if err == nil {
			return msg, nil
		}
		if authCanceled(err) || !IsMissingPackage(err) {
			return "", err
		}
		if !HasHelper(runner, "pacman") {
			return "", err
		}
	}

	if HasHelper(runner, "pacman") {
		return InstallOnce(runner, pkgname, "pacman")
	}

	if HasHelper(runner, "yay") {
		return InstallOnce(runner, pkgname, "yay")
	}

	return fmt.Sprintf("neither omarchy, pacman, nor yay found on PATH; run this yourself:\n  omarchy pkg add %s", pkgname), nil
}

// ErrPackageMissing is returned when the named package is not in the
// Omarchy/Arch repos the current helper can see.
var ErrPackageMissing = fmt.Errorf("not in the omarchy package repo")

// IsMissingPackage reports whether err is a "package not in the repos" failure,
// including the wrapped form Install/InstallOnce return to callers.
func IsMissingPackage(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrPackageMissing) {
		return true
	}
	return missingPackage(err)
}

// HasHelper reports whether name is on PATH according to runner.
func HasHelper(runner CommandRunner, name string) bool {
	if runner == nil {
		runner = DefaultRunner
	}
	_, err := runner.LookPath(name)
	return err == nil
}

// InstallOnce runs a single install backend: "omarchy", "pacman", or "yay".
// The TUI calls this per status-line step so it can show omarchy first,
// then pacman, instead of hiding both inside one blocking Install.
func InstallOnce(runner CommandRunner, pkgname, via string) (string, error) {
	if runner == nil {
		runner = DefaultRunner
	}
	switch via {
	case "omarchy":
		if path, err := runner.LookPath("omarchy-pkg-add"); err == nil {
			if err := elevate(runner, path, pkgname); err != nil {
				return "", installErr(pkgname, "omarchy", err)
			}
			return fmt.Sprintf("installed %s via omarchy", pkgname), nil
		}
		if path, err := runner.LookPath("omarchy"); err == nil {
			if err := elevate(runner, path, "pkg", "add", pkgname); err != nil {
				return "", installErr(pkgname, "omarchy", err)
			}
			return fmt.Sprintf("installed %s via omarchy", pkgname), nil
		}
		return "", fmt.Errorf("couldn't install %s via omarchy: omarchy not on PATH", pkgname)
	case "pacman":
		path, err := runner.LookPath("pacman")
		if err != nil {
			return "", fmt.Errorf("couldn't install %s via pacman: pacman not on PATH", pkgname)
		}
		if err := elevate(runner, path, "-S", "--noconfirm", "--needed", pkgname); err != nil {
			return "", installErr(pkgname, "pacman", err)
		}
		return fmt.Sprintf("installed %s via pacman", pkgname), nil
	case "yay":
		path, err := runner.LookPath("yay")
		if err != nil {
			return "", fmt.Errorf("couldn't install %s via yay: yay not on PATH", pkgname)
		}
		if err := runner.Run(path, "-S", "--noconfirm", "--needed", pkgname); err != nil {
			return "", installErr(pkgname, "yay", err)
		}
		return fmt.Sprintf("installed %s via yay", pkgname), nil
	default:
		return "", fmt.Errorf("couldn't install %s: unknown install via %q", pkgname, via)
	}
}

// InstallVia names the helper Install will use, for TUI status copy.
func InstallVia(runner CommandRunner) string {
	if runner == nil {
		runner = DefaultRunner
	}
	for _, name := range []string{"omarchy-pkg-add", "omarchy"} {
		if _, err := runner.LookPath(name); err == nil {
			return "omarchy"
		}
	}
	if _, err := runner.LookPath("pacman"); err == nil {
		return "pacman"
	}
	if _, err := runner.LookPath("yay"); err == nil {
		return "yay"
	}
	return "package manager"
}

func installErr(pkgname, via string, err error) error {
	if authCanceled(err) {
		return fmt.Errorf("couldn't install %s: authentication canceled", pkgname)
	}
	if missingPackage(err) {
		return fmt.Errorf("couldn't install %s: %w", pkgname, ErrPackageMissing)
	}
	return fmt.Errorf("couldn't install %s via %s: %w", pkgname, via, err)
}

// elevate runs argv via pkexec so Omarchy's polkit dialog handles the
// password, falling back to sudo only when pkexec is not installed.
func elevate(runner CommandRunner, argv ...string) error {
	if _, err := runner.LookPath("pkexec"); err == nil {
		return runner.Run("pkexec", argv...)
	}
	if _, err := runner.LookPath("sudo"); err == nil {
		return runner.Run("sudo", argv...)
	}
	return runner.Run(argv[0], argv[1:]...)
}

func missingPackage(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "target not found") ||
		strings.Contains(s, "did not install") ||
		strings.Contains(s, "could not find")
}

func authCanceled(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "dismissed") ||
		strings.Contains(s, "not authorized") ||
		strings.Contains(s, "authentication canceled") ||
		strings.Contains(s, "authorisation")
}
