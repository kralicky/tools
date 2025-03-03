// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package source

// This file defines the refactor.inline code action.

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"runtime/debug"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/types/typeutil"
	"golang.org/x/tools/gopls/pkg/bug"
	"golang.org/x/tools/gopls/pkg/file"
	"golang.org/x/tools/gopls/pkg/lsp/protocol"
	"golang.org/x/tools/gopls/pkg/lsp/safetoken"
	"golang.org/x/tools/pkg/diff"
	"golang.org/x/tools/pkg/event"
	"golang.org/x/tools/pkg/refactor/inline"
)

// EnclosingStaticCall returns the innermost function call enclosing
// the selected range, along with the callee.
func EnclosingStaticCall(pkg Package, pgf *ParsedGoFile, rng protocol.Range) (*ast.CallExpr, *types.Func, error) {
	start, end, err := pgf.RangePos(rng)
	if err != nil {
		return nil, nil, err
	}
	path, _ := astutil.PathEnclosingInterval(pgf.File, start, end)

	var call *ast.CallExpr
loop:
	for _, n := range path {
		switch n := n.(type) {
		case *ast.FuncLit:
			break loop
		case *ast.CallExpr:
			call = n
			break loop
		}
	}
	if call == nil {
		return nil, nil, fmt.Errorf("no enclosing call")
	}
	if safetoken.Line(pgf.Tok, call.Lparen) != safetoken.Line(pgf.Tok, start) {
		return nil, nil, fmt.Errorf("enclosing call is not on this line")
	}
	fn := typeutil.StaticCallee(pkg.GetTypesInfo(), call)
	if fn == nil {
		return nil, nil, fmt.Errorf("not a static call to a Go function")
	}
	return call, fn, nil
}

func inlineCall(ctx context.Context, snapshot Snapshot, fh file.Handle, rng protocol.Range) (_ []protocol.TextDocumentEdit, err error) {
	// Find enclosing static call.
	callerPkg, callerPGF, err := NarrowestPackageForFile(ctx, snapshot, fh.URI())
	if err != nil {
		return nil, err
	}
	call, fn, err := EnclosingStaticCall(callerPkg, callerPGF, rng)
	if err != nil {
		return nil, err
	}

	// Locate callee by file/line and analyze it.
	calleePosn := safetoken.StartPosition(callerPkg.FileSet(), fn.Pos())
	calleePkg, calleePGF, err := NarrowestPackageForFile(ctx, snapshot, protocol.URIFromPath(calleePosn.Filename))
	if err != nil {
		return nil, err
	}
	var calleeDecl *ast.FuncDecl
	for _, decl := range calleePGF.File.Decls {
		if decl, ok := decl.(*ast.FuncDecl); ok {
			posn := safetoken.StartPosition(calleePkg.FileSet(), decl.Name.Pos())
			if posn.Line == calleePosn.Line && posn.Column == calleePosn.Column {
				calleeDecl = decl
				break
			}
		}
	}
	if calleeDecl == nil {
		return nil, fmt.Errorf("can't find callee")
	}

	// The inliner assumes that input is well-typed,
	// but that is frequently not the case within gopls.
	// Until we are able to harden the inliner,
	// report panics as errors to avoid crashing the server.
	bad := func(p Package) bool { return len(p.GetParseErrors())+len(p.GetTypeErrors()) > 0 }
	if bad(calleePkg) || bad(callerPkg) {
		defer func() {
			if x := recover(); x != nil {
				err = bug.Errorf("inlining failed unexpectedly: %v\nstack: %v",
					x, debug.Stack())
			}
		}()
	}

	// Users can consult the gopls event log to see
	// why a particular inlining strategy was chosen.
	logf := logger(ctx, "inliner", snapshot.Options().VerboseOutput)

	callee, err := inline.AnalyzeCallee(logf, calleePkg.FileSet(), calleePkg.GetTypes(), calleePkg.GetTypesInfo(), calleeDecl, calleePGF.Src)
	if err != nil {
		return nil, err
	}

	// Inline the call.
	caller := &inline.Caller{
		Fset:    callerPkg.FileSet(),
		Types:   callerPkg.GetTypes(),
		Info:    callerPkg.GetTypesInfo(),
		File:    callerPGF.File,
		Call:    call,
		Content: callerPGF.Src,
	}

	got, err := inline.Inline(logf, caller, callee)
	if err != nil {
		return nil, err
	}

	return suggestedFixToEdits(ctx, snapshot, callerPkg.FileSet(), &analysis.SuggestedFix{
		Message:   fmt.Sprintf("inline call of %v", callee),
		TextEdits: diffToTextEdits(callerPGF.Tok, diff.Bytes(callerPGF.Src, got)),
	})
}

// TODO(adonovan): change the inliner to instead accept an io.Writer.
func logger(ctx context.Context, name string, verbose bool) func(format string, args ...any) {
	if verbose {
		return func(format string, args ...any) {
			event.Log(ctx, name+": "+fmt.Sprintf(format, args...))
		}
	} else {
		return func(string, ...any) {}
	}
}
