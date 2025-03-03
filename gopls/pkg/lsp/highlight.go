// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsp

import (
	"context"

	"golang.org/x/tools/gopls/pkg/file"
	"golang.org/x/tools/gopls/pkg/lsp/protocol"
	"golang.org/x/tools/gopls/pkg/lsp/source"
	"golang.org/x/tools/gopls/pkg/lsp/template"
	"golang.org/x/tools/pkg/event"
	"golang.org/x/tools/pkg/event/tag"
)

func (s *server) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	ctx, done := event.Start(ctx, "lsp.Server.documentHighlight", tag.URI.Of(params.TextDocument.URI))
	defer done()

	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.TextDocument.URI, file.Go)
	defer release()
	if !ok {
		return nil, err
	}

	if snapshot.FileKind(fh) == file.Tmpl {
		return template.Highlight(ctx, snapshot, fh, params.Position)
	}

	rngs, err := source.Highlight(ctx, snapshot, fh, params.Position)
	if err != nil {
		event.Error(ctx, "no highlight", err)
	}
	return toProtocolHighlight(rngs), nil
}

func toProtocolHighlight(rngs []protocol.Range) []protocol.DocumentHighlight {
	result := make([]protocol.DocumentHighlight, 0, len(rngs))
	kind := protocol.Text
	for _, rng := range rngs {
		result = append(result, protocol.DocumentHighlight{
			Kind:  kind,
			Range: rng,
		})
	}
	return result
}
