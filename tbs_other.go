// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, the go-tpm2/devtpm authors. All rights reserved.

//go:build !windows

package devtpm

import "github.com/go-tpm2/common"

// ErrTBSUnsupported is returned by OpenTBS off Windows, where there is no
// TPM Base Services. It lets a caller name the TBS device on every
// platform without a build tag of its own.
const ErrTBSUnsupported = common.Error("devtpm: TBS exists only on Windows")

// OpenTBS returns ErrTBSUnsupported: TPM Base Services is a Windows API.
func OpenTBS() (*TBS, error) { return nil, ErrTBSUnsupported }
