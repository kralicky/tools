// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsp

import (
	"context"

	"golang.org/x/tools/gopls/pkg/file"
	"golang.org/x/tools/gopls/pkg/lsp/protocol"
	"golang.org/x/tools/gopls/pkg/lsp/source"
	"golang.org/x/tools/pkg/event"
	"golang.org/x/tools/pkg/event/tag"
)

func (s *server) SignatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	ctx, done := event.Start(ctx, "lsp.Server.signatureHelp", tag.URI.Of(params.TextDocument.URI))
	defer done()

	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.TextDocument.URI, file.Go)
	defer release()
	if !ok {
		return nil, err
	}
	info, activeParameter, err := source.SignatureHelp(ctx, snapshot, fh, params.Position)
	if err != nil {
		event.Error(ctx, "no signature help", err, tag.Position.Of(params.Position))
		return nil, nil // sic? There could be many reasons for failure.
	}
	return &protocol.SignatureHelp{
		Signatures:      []protocol.SignatureInformation{*info},
		ActiveParameter: uint32(activeParameter),
	}, nil
}
