//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	module    = "github.com/openlibrecommunity/olcrtc"
	buildDir  = "build"
	ldflags   = "-s -w"
	goVersion = "1.25"
	bRepo     = "github.com/zarazaex69/b"
)

var (
	goexe  = mg.GoCmd()
	goos   = envOr("GOOS", runtime.GOOS)
	goarch = envOr("GOARCH", runtime.GOARCH)
)

// Build builds both olcrtc CLI and UI binaries.
func Build() error {
	mg.Deps(BuildCLI, BuildUI)
	return nil
}

// BuildCLI builds the olcrtc server/client binary.
func BuildCLI() error {
	mg.Deps(Deps)
	return buildBinary("olcrtc", "./cmd/olcrtc", goos, goarch, false)
}

// BuildB builds olcrtc with b codec support (requires libb.so).
func BuildB() error {
	mg.Deps(Deps)
	mg.Deps(B.Build)
	return buildBinary("olcrtc", "./cmd/olcrtc", goos, goarch, true)
}

// BuildUI builds the Fyne desktop UI binary.
func BuildUI() error {
	return buildUIBinary(goos, goarch)
}

// Cross builds olcrtc for all supported platforms.
func Cross() error {
	mg.Deps(Deps)

	targets := []struct{ os, arch string }{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"windows", "amd64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
		{"freebsd", "amd64"},
		{"freebsd", "arm64"},
		{"openbsd", "amd64"},
		{"openbsd", "arm64"},
	}

	for _, t := range targets {
		if err := buildBinary("olcrtc", "./cmd/olcrtc", t.os, t.arch, false); err != nil {
			return err
		}
	}

	return nil
}

// Podman builds the image using podman.
func Podman() error {
	tag := envOr("DOCKER_TAG", "olcrtc:latest")
	return sh.RunV("podman", "build", "-t", tag, ".")
}

// Docker builds the image using docker.
func Docker() error {
	tag := envOr("DOCKER_TAG", "olcrtc:latest")
	return sh.RunV("docker", "build", "-t", tag, ".")
}

// Lint runs golangci-lint.
func Lint() error {
	if err := ensureTool("golangci-lint"); err != nil {
		return fmt.Errorf("golangci-lint not found, install it:\n  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest")
	}
	return sh.RunV("golangci-lint", "run", "./...")
}

// Test runs all tests.
func Test() error {
	return sh.RunV(goexe, "test", "-race", "-count=1", "./...")
}

// Deps downloads and tidies Go module dependencies.
func Deps() error {
	if err := sh.RunV(goexe, "mod", "download"); err != nil {
		return err
	}
	return sh.RunV(goexe, "mod", "tidy")
}

// Clean removes build artifacts.
func Clean() error {
	return os.RemoveAll(buildDir)
}

// Mobile builds the Android AAR via gomobile.
func Mobile() error {
	if err := ensureTool("gomobile"); err != nil {
		return fmt.Errorf("gomobile not found: run 'go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init'")
	}
	if err := ensureBuildDir(); err != nil {
		return err
	}
	return sh.RunV("gomobile", "bind",
		"-target=android",
		"-androidapi", "21",
		"-ldflags", "-s -w -checklinkname=0",
		"-o", filepath.Join(buildDir, "olcrtc.aar"),
		"./mobile",
	)
}

func buildBinary(name, pkg, os_, arch string, withB bool) error {
	if err := ensureBuildDir(); err != nil {
		return err
	}

	ext := ""
	if os_ == "windows" {
		ext = ".exe"
	}
	suffix := ""
	if withB {
		suffix = "-b"
	}
	outName := fmt.Sprintf("%s%s-%s-%s%s", name, suffix, os_, arch, ext)
	out := filepath.Join(buildDir, outName)
	fmt.Printf("building %s (%s/%s, b=%v) -> %s\n", name, os_, arch, withB, out)

	env := map[string]string{
		"GOOS":   os_,
		"GOARCH": arch,
	}

	if withB {
		env["CGO_ENABLED"] = "1"
		bLibDir := bLibPath()
		env["CGO_LDFLAGS"] = fmt.Sprintf("-L%s -Wl,-rpath,%s", bLibDir, bLibDir)
	} else {
		env["CGO_ENABLED"] = "0"
	}

	flags := ldflags
	if os_ == "android" {
		flags += " -checklinkname=0"
	}

	args := []string{"build", "-trimpath", "-ldflags", flags}
	if withB {
		args = append(args, "-tags", "b")
	}
	args = append(args, "-o", out, pkg)

	return sh.RunWithV(env, goexe, args...)
}

func buildUIBinary(os_, arch string) error {
	if err := ensureBuildDir(); err != nil {
		return err
	}

	ext := ""
	if os_ == "windows" {
		ext = ".exe"
	}
	outName := fmt.Sprintf("%s-%s-%s%s", "olcrtc-ui", os_, arch, ext)
	absOut, err := filepath.Abs(filepath.Join(buildDir, outName))
	if err != nil {
		return err
	}

	fmt.Printf("building olcrtc-ui (%s/%s)\n", os_, arch)

	cmd := exec.Command(goexe, "build",
		"-trimpath",
		"-ldflags", ldflags,
		"-o", absOut,
		".",
	)
	cmd.Dir = "ui"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"GOOS="+os_,
		"GOARCH="+arch,
	)

	return cmd.Run()
}

type B mg.Namespace

func bLibPath() string {
	return filepath.Join(buildDir, "lib")
}

func bSrcPath() string {
	return filepath.Join(buildDir, "b-src")
}

func (B) Build() error {
	if err := ensureBuildDir(); err != nil {
		return err
	}

	libDir := bLibPath()
	libPath := filepath.Join(libDir, "libb.so")
	if _, err := os.Stat(libPath); err == nil {
		fmt.Println("libb.so already exists, skipping build")
		return nil
	}

	srcDir := bSrcPath()
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		fmt.Println("cloning b repository...")
		if err := sh.RunV("git", "clone", "--depth=1", "https://"+bRepo, srcDir); err != nil {
			return fmt.Errorf("failed to clone b: %w", err)
		}
	}

	fmt.Println("building libb.so with cargo...")
	if err := sh.RunV("cargo", "build", "--release", "--manifest-path", filepath.Join(srcDir, "Cargo.toml")); err != nil {
		return fmt.Errorf("cargo build failed: %w", err)
	}

	if err := os.MkdirAll(libDir, 0o755); err != nil {
		return err
	}

	srcLib := filepath.Join(srcDir, "target", "release", "libb.so")
	if err := sh.Copy(libPath, srcLib); err != nil {
		return fmt.Errorf("failed to copy libb.so: %w", err)
	}

	fmt.Printf("libb.so installed to %s\n", libPath)
	return nil
}

func (B) Clean() error {
	srcDir := bSrcPath()
	if _, err := os.Stat(srcDir); err == nil {
		if err := os.RemoveAll(srcDir); err != nil {
			return err
		}
	}
	libPath := filepath.Join(bLibPath(), "libb.so")
	if _, err := os.Stat(libPath); err == nil {
		return os.Remove(libPath)
	}
	return nil
}


func ensureBuildDir() error {
	return os.MkdirAll(buildDir, 0o755)
}

func ensureTool(name string) error {
	_, err := exec.LookPath(name)
	return err
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
