// Copyright (c) 2026, the go-zpkg/artifact authors
// All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package artifact

import "testing"

// ⛔ ANDROID MUST KEEP RESOLVING AS LINUX, AND THIS IS THE ONLY THING THAT
// SAYS SO TO A TOOL.
//
// osSlug names darwin, windows, js, wasip1 and the four BSDs, then falls
// through to "linux". Adding `android` to that list looks exactly like the
// tidy-up the BSDs received — and it is the opposite. The BSDs were named
// because the fallback's answer was WRONG for them: they got linux ELF objects
// they cannot load, discovered at the first exec. Android loads them.
//
// Measured on a Pixel 11 Pro Fold, Android 17 (arm64-v8a), 2026-11-14: a
// linux/aarch64 install of lz4.org with its gnu.org/glibc closure runs there,
// `zpkm run lz4.org -- --version` exit 0. So naming android would make a
// GOOS=android build resolve android/aarch64, for which nothing is published,
// and a platform that works today would begin refusing.
//
// go-zpkg/artifact#235 proposed precisely that change, before it was measured.
// A comment can be read past; this cannot.
func TestAndroidResolvesAsLinuxOnPurpose(t *testing.T) {
	if got := osSlug("android"); got != "linux" {
		t.Fatalf("osSlug(\"android\") = %q, want \"linux\".\n"+
			"If this was deliberate: naming android makes a GOOS=android build ask for\n"+
			"android/aarch64 artifacts, which nothing publishes, so an Android host that\n"+
			"installs and runs linux/aarch64 artifacts TODAY would start being refused.\n"+
			"Measured 2026-11-14 on a Pixel 11 Pro Fold; see host.go and go-zpkg/artifact#235.\n"+
			"Change this test only together with published android/aarch64 artifacts.", got)
	}
}

// And the fallback is not the thing most readers think they are changing: the
// published binary is built for linux, so it reports "linux" on a phone and
// never reaches the android case. Both spellings resolve to linux today, for
// two different reasons, and a tool should hold both.
func TestAGoosOfLinuxAndOfAndroidBothResolveToLinux(t *testing.T) {
	for _, goos := range []string{"linux", "android"} {
		if got := osSlug(goos); got != "linux" {
			t.Errorf("osSlug(%q) = %q, want \"linux\"", goos, got)
		}
	}
}
