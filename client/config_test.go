package client_test

import (
	"runtime"
	"testing"

	"github.com/aphexddb/omarket/client"
)

// setConfigDir points os.UserConfigDir() at dir for the duration of the test.
func setConfigDir(t *testing.T, dir string) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	setConfigDir(t, t.TempDir())

	if err := client.SaveConfig(client.Config{Server: "http://example.com"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cfg, err := client.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Server != "http://example.com" {
		t.Fatalf("Server = %q, want http://example.com", cfg.Server)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	setConfigDir(t, t.TempDir())

	cfg, err := client.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Server != "" {
		t.Fatalf("Server = %q, want empty", cfg.Server)
	}
}

func TestResolveServerPrecedence(t *testing.T) {
	setConfigDir(t, t.TempDir())

	if got := client.ResolveServer(""); got != client.DefaultServer {
		t.Fatalf("default: got %q want %q", got, client.DefaultServer)
	}

	if err := client.SaveConfig(client.Config{Server: "http://config.example"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if got := client.ResolveServer(""); got != "http://config.example" {
		t.Fatalf("config file: got %q", got)
	}

	t.Setenv("OMARKET_SERVER", "http://env.example")
	if got := client.ResolveServer(""); got != "http://env.example" {
		t.Fatalf("env: got %q", got)
	}

	if got := client.ResolveServer("http://flag.example"); got != "http://flag.example" {
		t.Fatalf("flag: got %q", got)
	}
}

func TestPageURL(t *testing.T) {
	if got := client.PageURL("https://omarket.dev", "hello-shareware"); got != "https://omarket.dev/a/hello-shareware" {
		t.Fatalf("PageURL: got %q", got)
	}
	// A trailing slash on the configured server must not double up.
	if got := client.PageURL("http://localhost:8484/", "my-app"); got != "http://localhost:8484/a/my-app" {
		t.Fatalf("PageURL with trailing slash: got %q", got)
	}
}
