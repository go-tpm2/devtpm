// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, the go-tpm2/devtpm authors. All rights reserved.

//go:build !windows

package devtpm

import "testing"

func TestOpenTBSOffWindows(t *testing.T) {
	tr, err := OpenTBS()
	if tr != nil || err != ErrTBSUnsupported {
		t.Fatalf("OpenTBS = %v, %v", tr, err)
	}
}
