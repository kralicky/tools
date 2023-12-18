// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"

	"golang.org/x/tools/gopls/pkg/file"
	"golang.org/x/tools/gopls/pkg/lsp/protocol"
	"golang.org/x/tools/gopls/pkg/lsp/source"
	"golang.org/x/tools/pkg/event"
)

func (s *server) PrepareCallHierarchy(ctx context.Context, params *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) {
	ctx, done := event.Start(ctx, "lsp.Server.prepareCallHierarchy")
	defer done()

	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.TextDocument.URI, file.Go)
	defer release()
	if !ok {
		return nil, err
	}

	return source.PrepareCallHierarchy(ctx, snapshot, fh, params.Position)
}

func (s *server) IncomingCalls(ctx context.Context, params *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error) {
	ctx, done := event.Start(ctx, "lsp.Server.incomingCalls")
	defer done()

	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.Item.URI, file.Go)
	defer release()
	if !ok {
		return nil, err
	}

	return source.IncomingCalls(ctx, snapshot, fh, params.Item.Range.Start)
}

func (s *server) OutgoingCalls(ctx context.Context, params *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) {
	ctx, done := event.Start(ctx, "lsp.Server.outgoingCalls")
	defer done()

	snapshot, fh, ok, release, err := s.beginFileRequest(ctx, params.Item.URI, file.Go)
	defer release()
	if !ok {
		return nil, err
	}

	return source.OutgoingCalls(ctx, snapshot, fh, params.Item.Range.Start)
}
