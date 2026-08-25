package client

import (
	"fmt"
	"os"
	"os/exec"
)

// CommandRunner abstracts process execution so install logic is testable
// without a real pacman/yay/sudo on the machine.
type CommandRunner interface {
	// Run executes name with args, wired to the caller's std streams.
	Run(name string, args ...string) error
	// LookPath reports whether name is found in PATH.
	LookPath(name string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (execRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// DefaultRunner shells out for real; swap in tests via Install's runner arg.
var DefaultRunner CommandRunner = execRunner{}

// Install installs pkgname per SPEC §2: prefer `sudo pacman -S --noconfirm`,
// fall back to `yay -S --noconfirm`, and if neither is available return an
// instructional message (not an error) telling the user what to run.
// A nil runner uses DefaultRunner.
func Install(runner CommandRunner, pkgname string) (string, error) {
	if runner == nil {
		runner = DefaultRunner
	}

	if _, err := runner.LookPath("pacman"); err == nil {
		if err := runner.Run("sudo", "pacman", "-S", "--noconfirm", pkgname); err != nil {
			return "", fmt.Errorf("pacman install failed: %w", err)
		}
		return fmt.Sprintf("installed %s via pacman", pkgname), nil
	}

	if _, err := runner.LookPath("yay"); err == nil {
		if err := runner.Run("yay", "-S", "--noconfirm", pkgname); err != nil {
			return "", fmt.Errorf("yay install failed: %w", err)
		}
		return fmt.Sprintf("installed %s via yay", pkgname), nil
	}

	return fmt.Sprintf("neither pacman nor yay found on PATH; run this yourself:\n  sudo pacman -S --noconfirm %s", pkgname), nil
}
