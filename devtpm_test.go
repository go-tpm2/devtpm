// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, the go-tpm2/devtpm authors. All rights reserved.

package devtpm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-tpm2/common"
)

// fake is an in-memory io.ReadWriteCloser standing in for a TPM character
// device or swtpm socket. It records the command bytes written, returns a
// canned response on read, and has knobs to inject every failure Send and
// Close must handle.
type fake struct {
	// written captures the bytes passed to the most recent Write.
	written []byte
	// resp is the canned response returned by Read.
	resp []byte

	// shortWrite, when set, makes Write report fewer bytes than it was
	// given (without an error), exercising the ErrShortWrite branch.
	shortWrite bool
	// writeErr, when non-nil, is returned by Write.
	writeErr error
	// readErr, when non-nil, is returned by Read.
	readErr error
	// closeErr, when non-nil, is returned by Close.
	closeErr error
}

func (f *fake) Write(p []byte) (int, error) {
	f.written = append([]byte(nil), p...)
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.shortWrite {
		return len(p) - 1, nil
	}
	return len(p), nil
}

func (f *fake) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	return copy(p, f.resp), nil
}

func (f *fake) Close() error { return f.closeErr }

// okResponse is a minimal well-formed TPM 2.0 response: TPM_ST_NO_SESSIONS,
// responseSize = HeaderSize, TPM_RC_SUCCESS.
func okResponse() []byte {
	r := common.PutU16(nil, uint16(common.TagNoSessions))
	r = common.PutU32(r, uint32(common.HeaderSize))
	r = common.PutU32(r, 0) // TPM_RC_SUCCESS
	return r
}

func TestNew(t *testing.T) {
	f := &fake{}
	tr := New(f)
	if tr == nil || tr.rwc != f {
		t.Fatalf("New did not wrap the provided ReadWriteCloser")
	}
}

func TestSendRoundTrip(t *testing.T) {
	want := okResponse()
	f := &fake{resp: want}
	tr := New(f)

	cmd := common.BuildCommand(uint16(common.TagNoSessions),
		uint32(common.CCGetRandom), []byte{0x00, 0x02})
	rsp, err := tr.Send(cmd)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !bytes.Equal(f.written, cmd) {
		t.Fatalf("device received %x, want %x", f.written, cmd)
	}
	if !bytes.Equal(rsp, want) {
		t.Fatalf("Send returned %x, want %x", rsp, want)
	}
	// The returned slice must be sized to the read count, not the full
	// maxResponse buffer.
	if len(rsp) != len(want) {
		t.Fatalf("Send returned %d bytes, want %d", len(rsp), len(want))
	}
}

func TestSendWriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	tr := New(&fake{writeErr: sentinel})
	_, err := tr.Send(okResponse())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Send err = %v, want %v", err, sentinel)
	}
}

func TestSendShortWrite(t *testing.T) {
	tr := New(&fake{shortWrite: true, resp: okResponse()})
	_, err := tr.Send(okResponse())
	if err != ErrShortWrite {
		t.Fatalf("Send err = %v, want ErrShortWrite", err)
	}
}

func TestSendReadError(t *testing.T) {
	sentinel := errors.New("read boom")
	tr := New(&fake{readErr: sentinel})
	_, err := tr.Send(okResponse())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Send err = %v, want %v", err, sentinel)
	}
}

func TestSendShortResponse(t *testing.T) {
	// A response shorter than a TPM 2.0 header (including the empty case)
	// must be rejected.
	for _, n := range []int{0, common.HeaderSize - 1} {
		f := &fake{resp: make([]byte, n)}
		_, err := New(f).Send(okResponse())
		if err != ErrShortResponse {
			t.Fatalf("Send(resp len %d) err = %v, want ErrShortResponse", n, err)
		}
	}
}

func TestOpenHappyPath(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tpmrm")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("Close temp: %v", err)
	}

	tr, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	if tr == nil {
		t.Fatalf("Open returned nil transport")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestOpenError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := Open(missing); err == nil {
		t.Fatalf("Open(%q) succeeded, want error", missing)
	}
}

func TestClose(t *testing.T) {
	if err := New(&fake{}).Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sentinel := errors.New("close boom")
	if err := New(&fake{closeErr: sentinel}).Close(); !errors.Is(err, sentinel) {
		t.Fatalf("Close err = %v, want %v", err, sentinel)
	}
}

// TestDefaultDevice pins the resource-manager device path.
func TestDefaultDevice(t *testing.T) {
	if DefaultDevice != "/dev/tpmrm0" {
		t.Fatalf("DefaultDevice = %q, want /dev/tpmrm0", DefaultDevice)
	}
}
