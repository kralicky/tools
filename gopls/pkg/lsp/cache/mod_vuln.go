// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"context"

	"golang.org/x/tools/gopls/pkg/lsp/protocol"
	"golang.org/x/tools/gopls/pkg/vulncheck"
	"golang.org/x/tools/gopls/pkg/vulncheck/scan"
	"golang.org/x/tools/pkg/memoize"
)

// ModVuln returns import vulnerability analysis for the given go.mod URI.
// Concurrent requests are combined into a single command.
func (s *Snapshot) ModVuln(ctx context.Context, modURI protocol.DocumentURI) (*vulncheck.Result, error) {
	s.mu.Lock()
	entry, hit := s.modVulnHandles.Get(modURI)
	s.mu.Unlock()

	type modVuln struct {
		result *vulncheck.Result
		err    error
	}

	// Cache miss?
	if !hit {
		handle := memoize.NewPromise("modVuln", func(ctx context.Context, arg interface{}) interface{} {
			result, err := scan.VulnerablePackages(ctx, arg.(*Snapshot))
			return modVuln{result, err}
		})

		entry = handle
		s.mu.Lock()
		s.modVulnHandles.Set(modURI, entry, nil)
		s.mu.Unlock()
	}

	// Await result.
	v, err := s.awaitPromise(ctx, entry)
	if err != nil {
		return nil, err
	}
	res := v.(modVuln)
	return res.result, res.err
}
