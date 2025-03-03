// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

// This file defines methods on DocumentURI.

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
)

// UnmarshalText implements decoding of DocumentURI values.
//
// In particular, it implements a systematic correction of various odd
// features of the definition of DocumentURI in the LSP spec that
// appear to be workarounds for bugs in VS Code. For example, it may
// URI-encode the URI itself, so that colon becomes %3A, and it may
// send file://foo.go URIs that have two slashes (not three) and no
// hostname.
//
// We use UnmarshalText, not UnmarshalJSON, because it is called even
// for non-addressable values such as keys and values of map[K]V,
// where there is no pointer of type *K or *V on which to call
// UnmarshalJSON. (See Go issue #28189 for more detail.)
//
// TODO(adonovan): should we reject all non-file DocumentURIs at decoding?
func (uri *DocumentURI) UnmarshalText(data []byte) error {
	fixed, err := fixDocumentURI(string(data))
	if err != nil {
		return err
	}
	*uri = DocumentURI(fixed)
	return nil
}

// IsFile reports whether the URI has "file" schema.
//
// (This is true for all current valid DocumentURIs. The protocol spec
// doesn't require it, but all known LSP clients identify editor
// documents with file URIs.)
func (uri DocumentURI) IsFile() bool {
	return strings.HasPrefix(string(uri), "file://")
}

// Path returns the file path for the given URI.
//
// Path panics if called on a URI that is not a valid filename.
func (uri DocumentURI) Path() string {
	filename, err := filename(uri)
	if err != nil {
		// e.g. ParseRequestURI failed.
		// TODO(adonovan): make this never panic,
		// and always return the best value it can.
		panic(err)
	}
	return filepath.FromSlash(filename)
}

func filename(uri DocumentURI) (string, error) {
	if uri == "" {
		return "", nil
	}

	// This conservative check for the common case
	// of a simple non-empty absolute POSIX filename
	// avoids the allocation of a net.URL.
	if strings.HasPrefix(string(uri), "file:///") {
		rest := string(uri)[len("file://"):] // leave one slash
		for i := 0; i < len(rest); i++ {
			b := rest[i]
			// Reject these cases:
			if b < ' ' || b == 0x7f || // control character
				b == '%' || b == '+' || // URI escape
				b == ':' || // Windows drive letter
				b == '@' || b == '&' || b == '?' { // authority or query
				goto slow
			}
		}
		return rest, nil
	}
slow:

	u, err := url.ParseRequestURI(string(uri))
	if err != nil {
		return "", err
	}
	if u.Scheme != fileScheme {
		return "", fmt.Errorf("only file URIs are supported, got %q from %q", u.Scheme, uri)
	}
	// If the URI is a Windows URI, we trim the leading "/" and uppercase
	// the drive letter, which will never be case sensitive.
	if isWindowsDriveURIPath(u.Path) {
		u.Path = strings.ToUpper(string(u.Path[1])) + u.Path[2:]
	}

	return u.Path, nil
}

// URIFromURI returns a DocumentURI, applying VS Code workarounds; see
// [DocumentURI.UnmarshalText] for details.
//
// TODO(adonovan): better name: FromWireURI? It's only used for
// sanitizing ParamInitialize.WorkspaceFolder.URIs from VS Code. Do
// they actually need this treatment?
func URIFromURI(s string) DocumentURI {
	fixed, err := fixDocumentURI(s)
	if err != nil {
		// TODO(adonovan): make this never panic.
		panic(err)
	}
	return DocumentURI(fixed)
}

// fixDocumentURI returns the fixed-up value of a DocumentURI field
// received from the LSP client; see [DocumentURI.UnmarshalText].
func fixDocumentURI(s string) (string, error) {
	if !strings.HasPrefix(s, "file://") {
		// TODO(adonovan): make this an error,
		// i.e. reject non-file URIs at ingestion?
		return s, nil
	}

	// VS Code sends URLs with only two slashes,
	// which are invalid. golang/go#39789.
	if !strings.HasPrefix(s, "file:///") {
		s = "file:///" + s[len("file://"):]
	}

	// Even though the input is a URI, it may not be in canonical form. VS Code
	// in particular over-escapes :, @, etc. Unescape and re-encode to canonicalize.
	path, err := url.PathUnescape(s[len("file://"):])
	if err != nil {
		return "", err
	}

	// File URIs from Windows may have lowercase drive letters.
	// Since drive letters are guaranteed to be case insensitive,
	// we change them to uppercase to remain consistent.
	// For example, file:///c:/x/y/z becomes file:///C:/x/y/z.
	if isWindowsDriveURIPath(path) {
		path = path[:1] + strings.ToUpper(string(path[1])) + path[2:]
	}
	u := url.URL{Scheme: fileScheme, Path: path}
	return u.String(), nil
}

// URIFromPath returns a "file"-scheme DocumentURI for the supplied
// file path. Given "", it returns "".
func URIFromPath(path string) DocumentURI {
	if path == "" {
		return ""
	}
	if !isWindowsDrivePath(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	// Check the file path again, in case it became absolute.
	if isWindowsDrivePath(path) {
		path = "/" + strings.ToUpper(string(path[0])) + path[1:]
	}
	path = filepath.ToSlash(path)
	u := url.URL{
		Scheme: fileScheme,
		Path:   path,
	}
	return DocumentURI(u.String())
}

const fileScheme = "file"

// isWindowsDrivePath returns true if the file path is of the form used by
// Windows. We check if the path begins with a drive letter, followed by a ":".
// For example: C:/x/y/z.
func isWindowsDrivePath(path string) bool {
	if len(path) < 3 {
		return false
	}
	return unicode.IsLetter(rune(path[0])) && path[1] == ':'
}

// isWindowsDriveURIPath returns true if the file URI is of the format used by
// Windows URIs. The url.Parse package does not specially handle Windows paths
// (see golang/go#6027), so we check if the URI path has a drive prefix (e.g. "/C:").
func isWindowsDriveURIPath(uri string) bool {
	if len(uri) < 4 {
		return false
	}
	return uri[0] == '/' && unicode.IsLetter(rune(uri[1])) && uri[2] == ':'
}
