// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsp

import (
	"context"
	"fmt"
	"sort"

	"golang.org/x/tools/gopls/pkg/file"
	"golang.org/x/tools/gopls/pkg/lsp/command"
	"golang.org/x/tools/gopls/pkg/lsp/mod"
	"golang.org/x/tools/gopls/pkg/lsp/protocol"
	"golang.org/x/tools/gopls/pkg/lsp/source"
	"golang.org/x/tools/pkg/event"
	"golang.org/x/tools/pkg/event/tag"
)

func (s *server) CodeLens(ctx context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	ctx, done := event.Start(ctx, "lsp.Server.codeLens", tag.URI.Of(params.TextDocument.URI))
	defer done()

	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.TextDocument.URI, file.UnknownKind)
	defer release()
	if !ok {
		return nil, err
	}
	var lenses map[command.Command]source.LensFunc
	switch snapshot.FileKind(fh) {
	case file.Mod:
		lenses = mod.LensFuncs()
	case file.Go:
		lenses = source.LensFuncs()
	default:
		// Unsupported file kind for a code lens.
		return nil, nil
	}
	var result []protocol.CodeLens
	for cmd, lf := range lenses {
		if !snapshot.Options().Codelenses[string(cmd)] {
			continue
		}
		added, err := lf(ctx, snapshot, fh)
		// Code lens is called on every keystroke, so we should just operate in
		// a best-effort mode, ignoring errors.
		if err != nil {
			event.Error(ctx, fmt.Sprintf("code lens %s failed", cmd), err)
			continue
		}
		result = append(result, added...)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if cmp := protocol.CompareRange(a.Range, b.Range); cmp != 0 {
			return cmp < 0
		}
		return a.Command.Command < b.Command.Command
	})
	return result, nil
}
