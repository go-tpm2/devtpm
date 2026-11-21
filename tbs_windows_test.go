// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, the go-tpm2/devtpm authors. All rights reserved.

//go:build windows

package devtpm

import (
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// stub replaces the DLL seams for one test.
func stub(t *testing.T, call func(*windows.LazyProc, ...uintptr) uint32) {
	t.Helper()
	c, f := tbsCall, tbsFind
	t.Cleanup(func() { tbsCall, tbsFind = c, f })
	tbsCall = call
	tbsFind = func() error { return nil }
}

func TestOpenTBSParams(t *testing.T) {
	var got tbsContextParams2
	stub(t, func(p *windows.LazyProc, a ...uintptr) uint32 {
		if p != procCtxCreate || len(a) != 2 {
			t.Fatalf("proc %v args %d", p.Name, len(a))
		}
		got = *(*tbsContextParams2)(toPtr(a[0]))
		*(*uintptr)(toPtr(a[1])) = 77
		return 0
	})
	tr, err := OpenTBS()
	if err != nil {
		t.Fatal(err)
	}
	if got.version != 2 || got.flags != 4 {
		t.Fatalf("params = %+v, want version 2, includeTpm20 (bit 2)", got)
	}
	if tr.b.(*winBackend).h != 77 {
		t.Fatal("handle not kept")
	}
}

func TestOpenTBSErrors(t *testing.T) {
	stub(t, func(*windows.LazyProc, ...uintptr) uint32 { return uint32(TBSErrTPMNotFound) })
	if _, err := OpenTBS(); err != TBSErrTPMNotFound {
		t.Fatalf("err = %v", err)
	}
	tbsFind = func() error { return errors.New("no dll") }
	if _, err := OpenTBS(); err != ErrTBSUnavailable {
		t.Fatalf("err = %v", err)
	}
}

func TestWinBackendSubmitAndClose(t *testing.T) {
	var sub []uintptr
	var closed uintptr
	stub(t, func(p *windows.LazyProc, a ...uintptr) uint32 {
		if p == procCtxClose {
			closed = a[0]
			return uint32(TBSErrInvalidContext)
		}
		if p != procSubmit {
			t.Fatalf("proc %s", p.Name)
		}
		sub = append([]uintptr(nil), a...)
		out := unsafe.Slice((*byte)(toPtr(a[5])), 2)
		out[0], out[1] = 9, 9
		*(*uint32)(toPtr(a[6])) = 2
		return 0
	})
	w := &winBackend{h: 5}
	out := make([]byte, 16)
	cmd := []byte{1, 2}
	n, code := w.submit(cmd, out)
	if n != 2 || code != 0 || out[0] != 9 {
		t.Fatalf("n=%d code=%v out=%v", n, code, out[:2])
	}
	if sub[0] != 5 || sub[1] != 0 || sub[2] != 200 || sub[4] != 2 {
		t.Fatalf("args = %v: want handle, locality 0, priority 200, cmd length 2", sub)
	}
	if code := w.close(); code != TBSErrInvalidContext || closed != 5 {
		t.Fatalf("close = %v, handle %d", code, closed)
	}
	// empty buffers never reach the DLL (&cmd[0] would panic).
	if _, code := w.submit(nil, out); code != TBSErrBadParameter {
		t.Fatalf("empty cmd: %v", code)
	}
	if _, code := w.submit(cmd, nil); code != TBSErrBadParameter {
		t.Fatalf("empty out: %v", code)
	}
}

// toPtr turns a uintptr argument captured by the stub back into a
// pointer, in the form go vet accepts for memory the test itself owns.
func toPtr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

// The real tbs.dll entry points must resolve on any Windows.
func TestTBSFindReal(t *testing.T) {
	if err := tbsFind(); err != nil {
		t.Fatal(err)
	}
	if err := findProcs(procSubmit, tbsDLL.NewProc("Tbs_No_Such_Entry")); err == nil {
		t.Fatal("a missing entry point was found")
	}
}

// The real DLL, against whatever TPM the machine has: a TBS error (no TPM:
// TBS_E_TPM_NOT_FOUND) or a working GetRandom, never a crash.
func TestRealTBS(t *testing.T) {
	tr, err := OpenTBS()
	if err != nil {
		var te TBSError
		if !errors.As(err, &te) {
			t.Fatalf("OpenTBS: %v is not a TBSError", err)
		}
		t.Logf("OpenTBS: %v", err)
		return
	}
	defer tr.Close()
	// TPM2_GetRandom, 8 bytes, no sessions.
	rsp, err := tr.Send([]byte{0x80, 0x01, 0, 0, 0, 12, 0, 0, 0x01, 0x7B, 0, 8})
	if err != nil {
		t.Fatalf("GetRandom: %v", err)
	}
	if len(rsp) < 10 {
		t.Fatalf("short response %x", rsp)
	}
}
