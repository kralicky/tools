// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package deepequalerrors_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/analysis/passes/deepequalerrors"
	"golang.org/x/tools/pkg/typeparams"
)

func Test(t *testing.T) {
	testdata := analysistest.TestData()
	tests := []string{"a"}
	if typeparams.Enabled {
		tests = append(tests, "typeparams")
	}
	analysistest.Run(t, testdata, deepequalerrors.Analyzer, tests...)
}
