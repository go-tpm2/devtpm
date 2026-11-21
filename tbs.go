// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, the go-tpm2/devtpm authors. All rights reserved.

package devtpm

import (
	"fmt"
	"sync"

	"github.com/go-tpm2/common"
)

// TBSError is a TBS (TPM Base Services) result code returned by tbs.dll,
// the only door a Windows user process has to the TPM. The values are the
// ones in Microsoft's "TBS Return Codes" table. A TBSError is comparable,
// so errors.Is(err, devtpm.TBSErrTPMNotFound) and err == ... both work.
//
// TBS_SUCCESS (0) is never returned as an error.
type TBSError uint32

// The TBS result codes (tbs.h), as documented at
// https://learn.microsoft.com/en-us/windows/win32/tbs/tbs-return-codes.
const (
	TBSErrInternalError       TBSError = 0x80284001
	TBSErrBadParameter        TBSError = 0x80284002
	TBSErrInvalidOutputPtr    TBSError = 0x80284003
	TBSErrInvalidContext      TBSError = 0x80284004
	TBSErrInsufficientBuffer  TBSError = 0x80284005
	TBSErrIOError             TBSError = 0x80284006
	TBSErrInvalidContextParam TBSError = 0x80284007
	TBSErrServiceNotRunning   TBSError = 0x80284008
	TBSErrTooManyContexts     TBSError = 0x80284009
	TBSErrTooManyResources    TBSError = 0x8028400A
	TBSErrServiceStartPending TBSError = 0x8028400B
	TBSErrPPINotSupported     TBSError = 0x8028400C
	TBSErrCommandCanceled     TBSError = 0x8028400D
	TBSErrBufferTooLarge      TBSError = 0x8028400E
	TBSErrTPMNotFound         TBSError = 0x8028400F
	TBSErrServiceDisabled     TBSError = 0x80284010
	TBSErrNoEventLog          TBSError = 0x80284011
	TBSErrAccessDenied        TBSError = 0x80284012
	TBSErrProvisioningDenied  TBSError = 0x80284013
	TBSErrPPIFuncUnsupported  TBSError = 0x80284014
	TBSErrOwnerAuthNotFound   TBSError = 0x80284015
	TBSErrProvisioningIncomp  TBSError = 0x80284016
)

var tbsNames = map[TBSError]string{
	TBSErrInternalError:       "TBS_E_INTERNAL_ERROR: an internal software error occurred",
	TBSErrBadParameter:        "TBS_E_BAD_PARAMETER: one or more parameter values are not valid",
	TBSErrInvalidOutputPtr:    "TBS_E_INVALID_OUTPUT_POINTER: a specified output pointer is bad",
	TBSErrInvalidContext:      "TBS_E_INVALID_CONTEXT: the context handle does not refer to a valid context",
	TBSErrInsufficientBuffer:  "TBS_E_INSUFFICIENT_BUFFER: the output buffer is too small",
	TBSErrIOError:             "TBS_E_IOERROR: an error occurred while communicating with the TPM",
	TBSErrInvalidContextParam: "TBS_E_INVALID_CONTEXT_PARAM: a context parameter that is not valid was passed",
	TBSErrServiceNotRunning:   "TBS_E_SERVICE_NOT_RUNNING: the TBS service is not running and could not be started",
	TBSErrTooManyContexts:     "TBS_E_TOO_MANY_TBS_CONTEXTS: too many open contexts",
	TBSErrTooManyResources:    "TBS_E_TOO_MANY_RESOURCES: too many open virtual resources",
	TBSErrServiceStartPending: "TBS_E_SERVICE_START_PENDING: the TBS service has been started but is not yet running",
	TBSErrPPINotSupported:     "TBS_E_PPI_NOT_SUPPORTED: the physical presence interface is not supported",
	TBSErrCommandCanceled:     "TBS_E_COMMAND_CANCELED: the command was canceled",
	TBSErrBufferTooLarge:      "TBS_E_BUFFER_TOO_LARGE: the input or output buffer is too large",
	TBSErrTPMNotFound:         "TBS_E_TPM_NOT_FOUND: no compatible TPM was found on this computer",
	TBSErrServiceDisabled:     "TBS_E_SERVICE_DISABLED: the TBS service has been disabled",
	TBSErrNoEventLog:          "TBS_E_NO_EVENT_LOG: the TBS event log is not available",
	TBSErrAccessDenied:        "TBS_E_ACCESS_DENIED: the caller does not have the rights for the operation",
	TBSErrProvisioningDenied:  "TBS_E_PROVISIONING_NOT_ALLOWED: the TPM provisioning action is not allowed",
	TBSErrPPIFuncUnsupported:  "TBS_E_PPI_FUNCTION_UNSUPPORTED: the firmware does not support the requested PPI method",
	TBSErrOwnerAuthNotFound:   "TBS_E_OWNERAUTH_NOT_FOUND: the requested TPM OwnerAuth value was not found",
	TBSErrProvisioningIncomp:  "TBS_E_PROVISIONING_INCOMPLETE: the TPM provisioning did not complete",
}

// Error implements error. Known codes carry the TBS constant name; an
// unknown code (a newer Windows) is printed as its hex value.
func (e TBSError) Error() string {
	if s, ok := tbsNames[e]; ok {
		return "tbs: " + s
	}
	return fmt.Sprintf("tbs: unknown result 0x%08X", uint32(e))
}

const (
	// ErrTBSClosed is returned by Send after Close.
	ErrTBSClosed = common.Error("devtpm: TBS context is closed")
	// ErrTBSBadLength is returned when TBS reports a response longer than
	// the buffer it was given, which a conforming TBS never does.
	ErrTBSBadLength = common.Error("devtpm: TBS reported a response longer than its buffer")
	// ErrTBSUnavailable is returned by OpenTBS when tbs.dll or one of its
	// entry points cannot be loaded.
	ErrTBSUnavailable = common.Error("devtpm: tbs.dll is not available")
)

// maxTBSResponse bounds the buffer Send grows to when TBS answers
// TBS_E_INSUFFICIENT_BUFFER with the size it needs: a hostile or buggy
// answer must not make us allocate without limit. It is sixteen times
// the 4096-byte TPM 2.0 maximum.
const maxTBSResponse = 16 * maxResponse

// tbsBackend is the seam over the three tbs.dll calls. submit receives
// the command and a response buffer whose length is the capacity offered
// to TBS; it returns the response length (or, with
// TBSErrInsufficientBuffer, the length TBS needs) and a result code, 0
// meaning TBS_SUCCESS. close releases the context.
type tbsBackend interface {
	submit(cmd, out []byte) (n int, code TBSError)
	close() TBSError
}

// TBS is a github.com/go-tpm2/common.Transport over Windows TPM Base
// Services: one TBS context, each command submitted at locality 0 and
// normal priority. TBS itself is the resource manager, so, as with
// /dev/tpmrm0, transient objects and sessions of a context are flushed
// when it is closed. It is safe for concurrent use; commands are
// serialised.
type TBS struct {
	mu     sync.Mutex
	b      tbsBackend
	closed bool
}

var _ common.Transport = (*TBS)(nil)

func newTBS(b tbsBackend) *TBS { return &TBS{b: b} }

// Send submits one marshaled TPM 2.0 command through Tbsip_Submit_Command
// and returns the whole response. A command the TPM rejects is not an
// error here: its TPM response code is in the returned bytes, as for every
// other transport. A TBS failure is returned as a TBSError.
func (t *TBS) Send(cmd []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTBSClosed
	}
	buf := make([]byte, maxResponse)
	n, code := t.b.submit(cmd, buf)
	if code == TBSErrInsufficientBuffer && n > len(buf) && n <= maxTBSResponse {
		buf = make([]byte, n)
		n, code = t.b.submit(cmd, buf)
	}
	if code != 0 {
		return nil, code
	}
	if n > len(buf) || n < 0 {
		return nil, ErrTBSBadLength
	}
	if n < common.HeaderSize {
		return nil, ErrShortResponse
	}
	return buf[:n], nil
}

// Close releases the TBS context (Tbsip_Context_Close). A second Close is
// a no-op returning nil.
func (t *TBS) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if code := t.b.close(); code != 0 {
		return code
	}
	return nil
}
