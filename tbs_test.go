// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, the go-tpm2/devtpm authors. All rights reserved.

package devtpm

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type fakeBackend struct {
	calls     int
	sizes     []int // len(out) offered on each submit
	rsp       []byte
	code      TBSError
	need      int // with code insufficient on the first call
	closeCode TBSError
	closed    int
}

func (f *fakeBackend) submit(cmd, out []byte) (int, TBSError) {
	f.calls++
	f.sizes = append(f.sizes, len(out))
	if f.need > 0 && f.calls == 1 {
		return f.need, TBSErrInsufficientBuffer
	}
	if f.code != 0 {
		return 0, f.code
	}
	return copy(out, f.rsp), 0
}

func (f *fakeBackend) close() TBSError { f.closed++; return f.closeCode }

var okRsp = []byte{0x80, 0x01, 0, 0, 0, 10, 0, 0, 0, 0}

func TestTBSSend(t *testing.T) {
	f := &fakeBackend{rsp: okRsp}
	tr := newTBS(f)
	got, err := tr.Send([]byte{1})
	if err != nil || !bytes.Equal(got, okRsp) {
		t.Fatalf("Send = %x, %v", got, err)
	}
	if f.sizes[0] != maxResponse {
		t.Fatalf("offered %d, want %d", f.sizes[0], maxResponse)
	}
}

func TestTBSSendGrowsOnce(t *testing.T) {
	f := &fakeBackend{rsp: okRsp, need: maxResponse + 1}
	got, err := newTBS(f).Send([]byte{1})
	if err != nil || !bytes.Equal(got, okRsp) {
		t.Fatalf("Send = %x, %v", got, err)
	}
	if f.calls != 2 || f.sizes[1] != maxResponse+1 {
		t.Fatalf("calls %d sizes %v", f.calls, f.sizes)
	}
}

func TestTBSSendRefusesUnboundedGrowth(t *testing.T) {
	for _, need := range []int{maxTBSResponse + 1, maxResponse, 0} {
		f := &fakeBackend{rsp: okRsp, need: need}
		if need == 0 {
			f.code = TBSErrInsufficientBuffer
		}
		_, err := newTBS(f).Send([]byte{1})
		if !errors.Is(err, TBSErrInsufficientBuffer) {
			t.Fatalf("need %d: err = %v", need, err)
		}
		if f.calls != 1 {
			t.Fatalf("need %d: %d calls, want 1", need, f.calls)
		}
	}
}

func TestTBSSendCodes(t *testing.T) {
	for code := range tbsNames {
		_, err := newTBS(&fakeBackend{code: code}).Send([]byte{1})
		if err != code {
			t.Fatalf("%08X: err = %v", uint32(code), err)
		}
	}
}

func TestTBSSendShortResponse(t *testing.T) {
	_, err := newTBS(&fakeBackend{rsp: okRsp[:5]}).Send([]byte{1})
	if err != ErrShortResponse {
		t.Fatalf("err = %v", err)
	}
}

type lyingBackend struct{ n int }

func (l lyingBackend) submit(cmd, out []byte) (int, TBSError) { return l.n, 0 }
func (l lyingBackend) close() TBSError                        { return 0 }

func TestTBSSendBadLength(t *testing.T) {
	for _, n := range []int{maxResponse + 1, -1} {
		if _, err := newTBS(lyingBackend{n}).Send([]byte{1}); err != ErrTBSBadLength {
			t.Fatalf("n=%d: err = %v", n, err)
		}
	}
}

func TestTBSClose(t *testing.T) {
	f := &fakeBackend{rsp: okRsp}
	tr := newTBS(f)
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil || f.closed != 1 {
		t.Fatalf("second Close = %v, backend closed %d times", err, f.closed)
	}
	if _, err := tr.Send([]byte{1}); err != ErrTBSClosed {
		t.Fatalf("Send after Close = %v", err)
	}
}

func TestTBSCloseError(t *testing.T) {
	tr := newTBS(&fakeBackend{closeCode: TBSErrInvalidContext})
	if err := tr.Close(); err != TBSErrInvalidContext {
		t.Fatalf("Close = %v", err)
	}
}

// The table of Microsoft's documented values: a wrong digit in a constant
// would map a real failure to the wrong error.
func TestTBSErrorValues(t *testing.T) {
	want := map[TBSError]struct {
		hex  uint32
		name string
	}{
		TBSErrInternalError:       {0x80284001, "TBS_E_INTERNAL_ERROR"},
		TBSErrBadParameter:        {0x80284002, "TBS_E_BAD_PARAMETER"},
		TBSErrInvalidOutputPtr:    {0x80284003, "TBS_E_INVALID_OUTPUT_POINTER"},
		TBSErrInvalidContext:      {0x80284004, "TBS_E_INVALID_CONTEXT"},
		TBSErrInsufficientBuffer:  {0x80284005, "TBS_E_INSUFFICIENT_BUFFER"},
		TBSErrIOError:             {0x80284006, "TBS_E_IOERROR"},
		TBSErrInvalidContextParam: {0x80284007, "TBS_E_INVALID_CONTEXT_PARAM"},
		TBSErrServiceNotRunning:   {0x80284008, "TBS_E_SERVICE_NOT_RUNNING"},
		TBSErrTooManyContexts:     {0x80284009, "TBS_E_TOO_MANY_TBS_CONTEXTS"},
		TBSErrTooManyResources:    {0x8028400A, "TBS_E_TOO_MANY_RESOURCES"},
		TBSErrServiceStartPending: {0x8028400B, "TBS_E_SERVICE_START_PENDING"},
		TBSErrPPINotSupported:     {0x8028400C, "TBS_E_PPI_NOT_SUPPORTED"},
		TBSErrCommandCanceled:     {0x8028400D, "TBS_E_COMMAND_CANCELED"},
		TBSErrBufferTooLarge:      {0x8028400E, "TBS_E_BUFFER_TOO_LARGE"},
		TBSErrTPMNotFound:         {0x8028400F, "TBS_E_TPM_NOT_FOUND"},
		TBSErrServiceDisabled:     {0x80284010, "TBS_E_SERVICE_DISABLED"},
		TBSErrNoEventLog:          {0x80284011, "TBS_E_NO_EVENT_LOG"},
		TBSErrAccessDenied:        {0x80284012, "TBS_E_ACCESS_DENIED"},
		TBSErrProvisioningDenied:  {0x80284013, "TBS_E_PROVISIONING_NOT_ALLOWED"},
		TBSErrPPIFuncUnsupported:  {0x80284014, "TBS_E_PPI_FUNCTION_UNSUPPORTED"},
		TBSErrOwnerAuthNotFound:   {0x80284015, "TBS_E_OWNERAUTH_NOT_FOUND"},
		TBSErrProvisioningIncomp:  {0x80284016, "TBS_E_PROVISIONING_INCOMPLETE"},
	}
	if len(want) != len(tbsNames) {
		t.Fatalf("%d codes tested, %d named", len(want), len(tbsNames))
	}
	for e, w := range want {
		if uint32(e) != w.hex {
			t.Errorf("%s = %08X, want %08X", w.name, uint32(e), w.hex)
		}
		if !strings.HasPrefix(e.Error(), "tbs: "+w.name+":") {
			t.Errorf("%08X: %q", w.hex, e.Error())
		}
	}
	if got := TBSError(0x80284999).Error(); got != "tbs: unknown result 0x80284999" {
		t.Fatalf("unknown: %q", got)
	}
}
