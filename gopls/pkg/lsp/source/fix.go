// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package source

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/gopls/pkg/bug"
	"golang.org/x/tools/gopls/pkg/file"
	"golang.org/x/tools/gopls/pkg/lsp/analysis/embeddirective"
	"golang.org/x/tools/gopls/pkg/lsp/analysis/fillstruct"
	"golang.org/x/tools/gopls/pkg/lsp/analysis/undeclaredname"
	"golang.org/x/tools/gopls/pkg/lsp/protocol"
	"golang.org/x/tools/gopls/pkg/settings"
	"golang.org/x/tools/pkg/imports"
)

type (
	// A suggestedFixFunc fixes diagnostics produced by the analysis framework.
	//
	// This is done outside of the analyzer Run function so that the construction
	// of expensive fixes can be deferred until they are requested by the user.
	//
	// TODO(rfindley): the signature of suggestedFixFunc should probably accept
	// (context.Context, Snapshot, protocol.Diagnostic). No reason for us to
	// encode as a (URI, Range) pair when we have the protocol type.
	suggestedFixFunc func(context.Context, Snapshot, file.Handle, protocol.Range) ([]protocol.TextDocumentEdit, error)
	suggestedFixer   struct {
		// fixesDiagnostic reports if a diagnostic from the analyzer can be fixed
		// by Fix. If nil then all diagnostics from the analyzer are assumed to be
		// fixable.
		canFix func(*Diagnostic) bool
		fix    suggestedFixFunc
	}
)

// suggestedFixes maps a suggested fix command id to its handler.
//
// TODO(adonovan): Every one of these fixers calls NarrowestPackageForFile as
// its first step and suggestedFixToEdits as its last. It might be a cleaner
// factoring of this historically very convoluted logic to move these two
// operations onto the caller side of the function interface, which would then
// have the type:
//
// type Fixer func(Context, Snapshot, Package, ParsedGoFile, Range) SuggestedFix, error
//
// Then remaining work done by the singleFile decorator becomes so trivial
// (just calling RangePos) that we can push it down into each singleFile fixer.
// All the fixers will then have a common and fully general interface, instead
// of the current two-tier system.
var suggestedFixes = map[settings.Fix]suggestedFixer{
	settings.FillStruct:        {fix: singleFile(fillstruct.SuggestedFix)},
	settings.UndeclaredName:    {fix: singleFile(undeclaredname.SuggestedFix)},
	settings.ExtractVariable:   {fix: singleFile(extractVariable)},
	settings.InlineCall:        {fix: inlineCall},
	settings.ExtractFunction:   {fix: singleFile(extractFunction)},
	settings.ExtractMethod:     {fix: singleFile(extractMethod)},
	settings.InvertIfCondition: {fix: singleFile(invertIfCondition)},
	settings.StubMethods:       {fix: stubSuggestedFixFunc},
	settings.AddEmbedImport: {
		canFix: fixedByImportingEmbed,
		fix:    addEmbedImport,
	},
}

type singleFileFixFunc func(fset *token.FileSet, start, end token.Pos, src []byte, file *ast.File, pkg *types.Package, info *types.Info) (*analysis.SuggestedFix, error)

// singleFile calls analyzers that expect inputs for a single file.
func singleFile(sf singleFileFixFunc) suggestedFixFunc {
	return func(ctx context.Context, snapshot Snapshot, fh file.Handle, rng protocol.Range) ([]protocol.TextDocumentEdit, error) {
		pkg, pgf, err := NarrowestPackageForFile(ctx, snapshot, fh.URI())
		if err != nil {
			return nil, err
		}
		start, end, err := pgf.RangePos(rng)
		if err != nil {
			return nil, err
		}
		fix, err := sf(pkg.FileSet(), start, end, pgf.Src, pgf.File, pkg.GetTypes(), pkg.GetTypesInfo())
		if err != nil {
			return nil, err
		}
		if fix == nil {
			return nil, nil
		}
		return suggestedFixToEdits(ctx, snapshot, pkg.FileSet(), fix)
	}
}

// CanFix returns true if Analyzer.Fix can fix the Diagnostic.
//
// It returns true by default: only if the analyzer is configured explicitly to
// ignore this diagnostic does it return false.
//
// TODO(rfindley): reconcile the semantics of 'Fix' and
// 'suggestedAnalysisFixes'.
func CanFix(a *settings.Analyzer, d *Diagnostic) bool {
	fixer, ok := suggestedFixes[a.Fix]
	if !ok || fixer.canFix == nil {
		// See the above TODO: this doesn't make sense, but preserves pre-existing
		// semantics.
		return true
	}
	return fixer.canFix(d)
}

// ApplyFix applies the command's suggested fix to the given file and
// range, returning the resulting edits.
func ApplyFix(ctx context.Context, fix settings.Fix, snapshot Snapshot, fh file.Handle, rng protocol.Range) ([]protocol.TextDocumentEdit, error) {
	fixer, ok := suggestedFixes[fix]
	if !ok {
		return nil, fmt.Errorf("no suggested fix function for %s", fix)
	}
	return fixer.fix(ctx, snapshot, fh, rng)
}

func suggestedFixToEdits(ctx context.Context, snapshot Snapshot, fset *token.FileSet, suggestion *analysis.SuggestedFix) ([]protocol.TextDocumentEdit, error) {
	editsPerFile := map[protocol.DocumentURI]*protocol.TextDocumentEdit{}
	for _, edit := range suggestion.TextEdits {
		tokFile := fset.File(edit.Pos)
		if tokFile == nil {
			return nil, bug.Errorf("no file for edit position")
		}
		end := edit.End
		if !end.IsValid() {
			end = edit.Pos
		}
		fh, err := snapshot.ReadFile(ctx, protocol.URIFromPath(tokFile.Name()))
		if err != nil {
			return nil, err
		}
		te, ok := editsPerFile[fh.URI()]
		if !ok {
			te = &protocol.TextDocumentEdit{
				TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
					Version: fh.Version(),
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{
						URI: fh.URI(),
					},
				},
			}
			editsPerFile[fh.URI()] = te
		}
		content, err := fh.Content()
		if err != nil {
			return nil, err
		}
		m := protocol.NewMapper(fh.URI(), content)
		rng, err := m.PosRange(tokFile, edit.Pos, end)
		if err != nil {
			return nil, err
		}
		te.Edits = append(te.Edits, protocol.TextEdit{
			Range:   rng,
			NewText: string(edit.NewText),
		})
	}
	var edits []protocol.TextDocumentEdit
	for _, edit := range editsPerFile {
		edits = append(edits, *edit)
	}
	return edits, nil
}

// fixedByImportingEmbed returns true if diag can be fixed by addEmbedImport.
func fixedByImportingEmbed(diag *Diagnostic) bool {
	if diag == nil {
		return false
	}
	return diag.Message == embeddirective.MissingImportMessage
}

// addEmbedImport adds a missing embed "embed" import with blank name.
func addEmbedImport(ctx context.Context, snapshot Snapshot, fh file.Handle, _ protocol.Range) ([]protocol.TextDocumentEdit, error) {
	pkg, pgf, err := NarrowestPackageForFile(ctx, snapshot, fh.URI())
	if err != nil {
		return nil, fmt.Errorf("narrow pkg: %w", err)
	}

	// Like source.AddImport, but with _ as Name and using our pgf.
	protoEdits, err := ComputeOneImportFixEdits(snapshot, pgf, &imports.ImportFix{
		StmtInfo: imports.ImportInfo{
			ImportPath: "embed",
			Name:       "_",
		},
		FixType: imports.AddImport,
	})
	if err != nil {
		return nil, fmt.Errorf("compute edits: %w", err)
	}

	var edits []analysis.TextEdit
	for _, e := range protoEdits {
		start, end, err := pgf.RangePos(e.Range)
		if err != nil {
			return nil, fmt.Errorf("map range: %w", err)
		}
		edits = append(edits, analysis.TextEdit{
			Pos:     start,
			End:     end,
			NewText: []byte(e.NewText),
		})
	}

	fix := &analysis.SuggestedFix{
		Message:   "Add embed import",
		TextEdits: edits,
	}
	return suggestedFixToEdits(ctx, snapshot, pkg.FileSet(), fix)
}
