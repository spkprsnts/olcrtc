package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/app/session"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

func TestToSessionConfigAndFirstNonEmpty(t *testing.T) {
	cfg := config{
		mode:            "cnc",
		link:            "direct",
		transport:       "vp8channel",
		provider:        "jazz",
		roomID:          "room",
		clientID:        "client",
		keyHex:          "key",
		socksHost:       "127.0.0.1",
		socksPort:       1080,
		dnsServer:       "1.1.1.1:53",
		socksProxyAddr:  "proxy",
		socksProxyPort:  1081,
		videoWidth:      640,
		videoHeight:     480,
		videoFPS:        30,
		videoBitrate:    "1M",
		videoHW:         "none",
		videoQRSize:     4,
		videoQRRecovery: "low",
		videoCodec:      "qrcode",
		videoTileModule: 4,
		videoTileRS:     20,
		vp8FPS:          25,
		vp8BatchSize:    8,
	}

	got := toSessionConfig(cfg)
	if got.Mode != cfg.mode || got.Carrier != "jazz" || got.SOCKSPort != cfg.socksPort ||
		got.VideoTileRS != cfg.videoTileRS || got.VP8BatchSize != cfg.vp8BatchSize {
		t.Fatalf("toSessionConfig() = %+v", got)
	}

	cfg.carrier = "telemost"
	got = toSessionConfig(cfg)
	if got.Carrier != "telemost" {
		t.Fatalf("carrier precedence = %q, want telemost", got.Carrier)
	}

	if got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Fatalf("firstNonEmpty() = %q, want x", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty(empty) = %q, want empty", got)
	}
}

func TestConfigureLogging(t *testing.T) {
	logger.SetVerbose(false)
	configureLogging(true)
	if !logger.IsVerbose() {
		t.Fatal("configureLogging(true) did not enable verbose logging")
	}

	logger.SetVerbose(false)
	configureLogging(false)
	if logger.IsVerbose() {
		t.Fatal("configureLogging(false) enabled verbose logging")
	}
}

func TestResolveDataDir(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "data")
	got, err := resolveDataDir(abs)
	if err != nil {
		t.Fatalf("resolveDataDir(abs) error = %v", err)
	}
	if got != abs {
		t.Fatalf("resolveDataDir(abs) = %q, want %q", got, abs)
	}

	got, err = resolveDataDir("data")
	if err != nil {
		t.Fatalf("resolveDataDir(rel) error = %v", err)
	}
	if filepath.Base(got) != "data" || !filepath.IsAbs(got) {
		t.Fatalf("resolveDataDir(rel) = %q, want absolute path ending in data", got)
	}
}

func TestLoadNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "names"), []byte("A\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(names) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "surnames"), []byte("B\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(surnames) error = %v", err)
	}
	if err := loadNames(dir); err != nil {
		t.Fatalf("loadNames() error = %v", err)
	}
}

func TestWaitForShutdown(t *testing.T) {
	errCh := make(chan error, 1)
	errCh <- nil
	if err := waitForShutdown(errCh); err != nil {
		t.Fatalf("waitForShutdown(nil) error = %v", err)
	}

	want := errors.New("boom")
	errCh = make(chan error, 1)
	errCh <- want
	if err := waitForShutdown(errCh); !errors.Is(err, want) {
		t.Fatalf("waitForShutdown(error) = %v, want %v", err, want)
	}
}

func TestValidateConfigAliasStillValidates(t *testing.T) {
	session.RegisterDefaults()
	cfg := config{
		mode:       "srv",
		link:       "direct",
		transport:  "datachannel",
		provider:   "jazz",
		clientID:   "client",
		keyHex:     "key",
		dnsServer:  "1.1.1.1:53",
		videoCodec: "qrcode",
	}
	if err := session.Validate(toSessionConfig(cfg)); err != nil {
		t.Fatalf("Validate(toSessionConfig(alias)) error = %v", err)
	}
}
