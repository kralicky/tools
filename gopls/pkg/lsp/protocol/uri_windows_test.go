// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows
// +build windows

package protocol_test

import (
	"testing"

	"golang.org/x/tools/gopls/pkg/lsp/protocol"
)

// TestURI tests the conversion between URIs and filenames. The test cases
// include Windows-style URIs and filepaths, but we avoid having OS-specific
// tests by using only forward slashes, assuming that the standard library
// functions filepath.ToSlash and filepath.FromSlash do not need testing.
func TestURIFromPath(t *testing.T) {
	for _, test := range []struct {
		path, wantFile string
		wantURI        protocol.DocumentURI
	}{
		{
			path:     ``,
			wantFile: ``,
			wantURI:  protocol.DocumentURI(""),
		},
		{
			path:     `C:\Windows\System32`,
			wantFile: `C:\Windows\System32`,
			wantURI:  protocol.DocumentURI("file:///C:/Windows/System32"),
		},
		{
			path:     `C:\Go\src\bob.go`,
			wantFile: `C:\Go\src\bob.go`,
			wantURI:  protocol.DocumentURI("file:///C:/Go/src/bob.go"),
		},
		{
			path:     `c:\Go\src\bob.go`,
			wantFile: `C:\Go\src\bob.go`,
			wantURI:  protocol.DocumentURI("file:///C:/Go/src/bob.go"),
		},
		{
			path:     `\path\to\dir`,
			wantFile: `C:\path\to\dir`,
			wantURI:  protocol.DocumentURI("file:///C:/path/to/dir"),
		},
		{
			path:     `\a\b\c\src\bob.go`,
			wantFile: `C:\a\b\c\src\bob.go`,
			wantURI:  protocol.DocumentURI("file:///C:/a/b/c/src/bob.go"),
		},
		{
			path:     `c:\Go\src\bob george\george\george.go`,
			wantFile: `C:\Go\src\bob george\george\george.go`,
			wantURI:  protocol.DocumentURI("file:///C:/Go/src/bob%20george/george/george.go"),
		},
	} {
		got := protocol.URIFromPath(test.path)
		if got != test.wantURI {
			t.Errorf("URIFromPath(%q): got %q, expected %q", test.path, got, test.wantURI)
		}
		gotFilename := got.Path()
		if gotFilename != test.wantFile {
			t.Errorf("Filename(%q): got %q, expected %q", got, gotFilename, test.wantFile)
		}
	}
}

func TestURIFromURI(t *testing.T) {
	for _, test := range []struct {
		inputURI, wantFile string
		wantURI            protocol.DocumentURI
	}{
		{
			inputURI: `file:///c:/Go/src/bob%20george/george/george.go`,
			wantFile: `C:\Go\src\bob george\george\george.go`,
			wantURI:  protocol.DocumentURI("file:///C:/Go/src/bob%20george/george/george.go"),
		},
		{
			inputURI: `file:///C%3A/Go/src/bob%20george/george/george.go`,
			wantFile: `C:\Go\src\bob george\george\george.go`,
			wantURI:  protocol.DocumentURI("file:///C:/Go/src/bob%20george/george/george.go"),
		},
		{
			inputURI: `file:///c:/path/to/%25p%25ercent%25/per%25cent.go`,
			wantFile: `C:\path\to\%p%ercent%\per%cent.go`,
			wantURI:  protocol.DocumentURI(`file:///C:/path/to/%25p%25ercent%25/per%25cent.go`),
		},
		{
			inputURI: `file:///C%3A/`,
			wantFile: `C:\`,
			wantURI:  protocol.DocumentURI(`file:///C:/`),
		},
		{
			inputURI: `file:///`,
			wantFile: `\`,
			wantURI:  protocol.DocumentURI(`file:///`),
		},
	} {
		got := protocol.URIFromURI(test.inputURI)
		if got != test.wantURI {
			t.Errorf("NewURI(%q): got %q, expected %q", test.inputURI, got, test.wantURI)
		}
		gotFilename := got.Path()
		if gotFilename != test.wantFile {
			t.Errorf("Filename(%q): got %q, expected %q", got, gotFilename, test.wantFile)
		}
	}
}
