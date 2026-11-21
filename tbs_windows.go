// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, the go-tpm2/devtpm authors. All rights reserved.

//go:build windows

package devtpm

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Constants from tbs.h, as documented in Microsoft's Tbsi_Context_Create,
// TBS_CONTEXT_PARAMS2 and Tbsip_Submit_Command pages.
const (
	tpmVersion20      = 2 // TPM_VERSION_20
	includeTpm20      = 1 << 2
	tbsLocalityZero   = 0   // TBS_COMMAND_LOCALITY_ZERO
	tbsPriorityNormal = 200 // TBS_COMMAND_PRIORITY_NORMAL (0xC8)
)

// tbsContextParams2 is TBS_CONTEXT_PARAMS2: version, then the bit field
// (requestRaw bit 0, includeTpm12 bit 1, includeTpm20 bit 2) as a UINT32.
type tbsContextParams2 struct {
	version uint32
	flags   uint32
}

var (
	tbsDLL        = windows.NewLazySystemDLL("tbs.dll")
	procCtxCreate = tbsDLL.NewProc("Tbsi_Context_Create")
	procSubmit    = tbsDLL.NewProc("Tbsip_Submit_Command")
	procCtxClose  = tbsDLL.NewProc("Tbsip_Context_Close")
)

// tbsCall is the one seam over the DLL: it invokes a tbs.dll entry point
// and returns the raw TBS_RESULT. Tests replace it; everything above it is
// ordinary code.
var tbsCall = func(p *windows.LazyProc, args ...uintptr) uint32 {
	r, _, _ := p.Call(args...)
	return uint32(r)
}

// tbsFind reports whether the entry points resolve. Tests replace it.
var tbsFind = func() error { return findProcs(procCtxCreate, procSubmit, procCtxClose) }

func findProcs(ps ...*windows.LazyProc) error {
	for _, p := range ps {
		if err := p.Find(); err != nil {
			return err
		}
	}
	return nil
}

type winBackend struct{ h uintptr }

func (w *winBackend) submit(cmd, out []byte) (int, TBSError) {
	if len(cmd) == 0 || len(out) == 0 {
		return 0, TBSErrBadParameter
	}
	n := uint32(len(out))
	code := tbsCall(procSubmit, w.h, tbsLocalityZero, tbsPriorityNormal,
		uintptr(unsafe.Pointer(&cmd[0])), uintptr(len(cmd)),
		uintptr(unsafe.Pointer(&out[0])), uintptr(unsafe.Pointer(&n)))
	return int(n), TBSError(code)
}

func (w *winBackend) close() TBSError { return TBSError(tbsCall(procCtxClose, w.h)) }

// OpenTBS creates a TBS context for a TPM 2.0 (Tbsi_Context_Create with
// TBS_CONTEXT_PARAMS2, version TPM_VERSION_20, includeTpm20) and returns
// the Transport over it. On a machine with no TPM 2.0 it returns
// TBSErrTPMNotFound; with the TBS service stopped or disabled,
// TBSErrServiceNotRunning or TBSErrServiceDisabled. Close the Transport
// to release the context.
func OpenTBS() (*TBS, error) {
	if err := tbsFind(); err != nil {
		return nil, ErrTBSUnavailable
	}
	p := tbsContextParams2{version: tpmVersion20, flags: includeTpm20}
	var h uintptr
	if code := tbsCall(procCtxCreate, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&h))); code != 0 {
		return nil, TBSError(code)
	}
	return newTBS(&winBackend{h: h}), nil
}
