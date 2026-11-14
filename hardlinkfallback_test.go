// Copyright (c) 2026, the go-zpkg/artifact authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package artifact

import (
	"archive/tar"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// linkPair is the shape every test here extracts: a file and a hard link to it.
func linkPair(t *testing.T) []byte {
	t.Helper()
	return buildExtractTar(t, []tarEntry{
		{name: "bin/getconf", typ: tar.TypeReg, mode: 0o755, body: "the real bytes"},
		{name: "libexec/XBS5_LP64_OFF64", typ: tar.TypeLink, mode: 0o755, link: "bin/getconf"},
	})
}

// nlink reports how many names the file has, which is the only observable that
// tells a hard link from a copy.
func nlink(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skipf("no syscall.Stat_t on this platform, cannot count names for %s", path)
	}
	return uint64(st.Nlink)
}

// linkWorks reports whether this filesystem will create a hard link at all.
//
// ⛔ IT IS PROBED, NOT ASSUMED FROM THE PLATFORM. The test below asserts that
// a real hard link is still made where link(2) is allowed, and that assertion
// is false wherever it is not — Android's /data denies it, and so does
// wasip1 under wasmtime, which is how this was found: the suite was green on
// darwin and linux and red on the wasm lane, for the very reason the change
// is about. A GOOS list would have to be kept in step with every runtime's
// policy; one syscall answers for the filesystem actually under the test.
func linkWorks(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "probe")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing the probe: %v", err)
	}
	return os.Link(src, filepath.Join(dir, "probe-link")) == nil
}

// A filesystem that allows link(2) must still get a real hard link: the
// fallback is for the refusal, not a replacement for the mechanism.
func TestAHardLinkIsStillAHardLinkWhereLinkIsAllowed(t *testing.T) {
	if !linkWorks(t) {
		t.Skip("link(2) is refused on this filesystem, which is the case the FALLBACK covers")
	}
	dest := t.TempDir()
	if err := extract(linkPair(t), dest, 0); err != nil {
		t.Fatalf("extract: %v", err)
	}
	target := filepath.Join(dest, "libexec/XBS5_LP64_OFF64")
	if n := nlink(t, target); n != 2 {
		t.Errorf("st_nlink = %d, want 2: the entry was copied where it could have been linked", n)
	}
}

// ⛔ THE DEFECT THIS EXISTS FOR. link(2) is denied on Android's /data
// (protected_hardlinks/SELinux), measured on a Pixel 11 Pro Fold on
// 2026-11-14, and gnu.org/glibc hard-links its getconf variants — so one
// refused syscall made the whole implicit libc closure unreachable on an
// archive that unpacks everywhere else. A hard link is an optimisation; the
// archive's intent is that two paths hold the same content, and a copy says
// that too.
func TestALinkRefusedBecomesACopyWithTheSameContent(t *testing.T) {
	old := extLink
	t.Cleanup(func() { extLink = old })
	extLink = func(string, string) error { return syscall.EPERM }

	dest := t.TempDir()
	if err := extract(linkPair(t), dest, 0); err != nil {
		t.Fatalf("extract refused to fall back: %v", err)
	}

	target := filepath.Join(dest, "libexec/XBS5_LP64_OFF64")
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the copy is not there: %v", err)
	}
	if string(b) != "the real bytes" {
		t.Errorf("content = %q, want the source's bytes", b)
	}
	// A copy, not a link: one name, so this really took the fallback rather
	// than a link that happened to work.
	if n := nlink(t, target); n != 1 {
		t.Errorf("st_nlink = %d, want 1: this should be a copy", n)
	}
	// ⛔ AND NOT A SYMLINK, WHICH IS THE ASSERTION EVERYTHING ABOVE MISSES.
	// Every check so far goes through os.Stat, which FOLLOWS a symlink: the
	// content matches, the mode matches, and the source's st_nlink is 1 either
	// way. Replacing the copy with os.Symlink therefore passed this test whole
	// — measured, by making that change and watching it stay green. Only
	// Lstat can tell them apart, and the difference is observable to every
	// consumer: readlink, a dangling target if the tree is moved, and a
	// relative path resolved against the wrong directory.
	lfi, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if lfi.Mode()&os.ModeSymlink != 0 {
		t.Error("the fallback produced a SYMLINK; the archive's intent is the bytes, and a symlink is observably different")
	}
	// The mode comes from the SOURCE file, because a hard link shares an inode
	// and therefore its permissions.
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o755 {
		t.Errorf("perm = %o, want 755 — taken from the source, not the link header", got)
	}
}

// ⛔ BOTH ERRORS TRAVEL. Told only about the copy, a reader hunts a disk or
// permission problem and never learns that link(2) is refused here — the one
// fact that explains every other symptom on the platform.
func TestWhenTheFallbackAlsoFailsBothErrorsAreReported(t *testing.T) {
	oldLink, oldCopy := extLink, extCopyLink
	t.Cleanup(func() { extLink, extCopyLink = oldLink, oldCopy })
	extLink = func(string, string) error { return syscall.EPERM }
	extCopyLink = func(string, string) error { return errors.New("no space left on device") }

	err := extract(linkPair(t), t.TempDir(), 0)
	if err == nil {
		t.Fatal("a failed link and a failed copy produced no error")
	}
	if !errors.Is(err, syscall.EPERM) {
		t.Errorf("error %v does not wrap the link failure, so errors.Is cannot see it", err)
	}
	for _, want := range []string{"no space left on device", "XBS5_LP64_OFF64", "getconf"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// The fallback reads the source, so a vanished source is its own error rather
// than a silent empty file.
func TestTheFallbackReportsAMissingSource(t *testing.T) {
	old := extLink
	t.Cleanup(func() { extLink = old })
	extLink = func(string, string) error { return syscall.EPERM }

	// A hard link whose source is never written by the archive.
	data := buildExtractTar(t, []tarEntry{
		{name: "libexec/orphan", typ: tar.TypeLink, mode: 0o755, link: "bin/absent"},
	})
	err := extract(data, t.TempDir(), 0)
	if err == nil {
		t.Fatal("a hard link to a file the archive never wrote was accepted")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error %v does not say the source is missing", err)
	}
}
