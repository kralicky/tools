// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmd

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"unicode/utf8"

	"golang.org/x/tools/gopls/pkg/lsp/protocol"
	"golang.org/x/tools/gopls/pkg/settings"
)

// generate semantic tokens and interpolate them in the file

// The output is the input file decorated with comments showing the
// syntactic tokens. The comments are stylized:
//   /*<arrow><length>,<token type>,[<modifiers]*/
// For most occurrences, the comment comes just before the token it
// describes, and arrow is a right arrow. If the token is inside a string
// the comment comes just after the string, and the arrow is a left arrow.
// <length> is the length of the token in runes, <token type> is one
// of the supported semantic token types, and <modifiers. is a
// (possibly empty) list of token type modifiers.

// There are 3 coordinate systems for lines and character offsets in lines
// LSP (what's returned from semanticTokens()):
//    0-based: the first line is line 0, the first character of a line
//      is character 0, and characters are counted as UTF-16 code points
// gopls (and Go error messages):
//    1-based: the first line is line1, the first character of a line
//      is character 0, and characters are counted as bytes
// internal (as used in marks, and lines:=bytes.Split(buf, '\n'))
//    0-based: lines and character positions are 1 less than in
//      the gopls coordinate system

type semtok struct {
	app *Application
}

func (c *semtok) Name() string      { return "semtok" }
func (c *semtok) Parent() string    { return c.app.Name() }
func (c *semtok) Usage() string     { return "<filename>" }
func (c *semtok) ShortHelp() string { return "show semantic tokens for the specified file" }
func (c *semtok) DetailedHelp(f *flag.FlagSet) {
	fmt.Fprint(f.Output(), `
Example: show the semantic tokens for this file:

	$ gopls semtok internal/cmd/semtok.go
`)
	printFlagDefaults(f)
}

// Run performs the semtok on the files specified by args and prints the
// results to stdout in the format described above.
func (c *semtok) Run(ctx context.Context, args ...string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected one file name, got %d", len(args))
	}
	// perhaps simpler if app had just had a FlagSet member
	origOptions := c.app.options
	c.app.options = func(opts *settings.Options) {
		origOptions(opts)
		opts.SemanticTokens = true
	}
	conn, err := c.app.connect(ctx, nil)
	if err != nil {
		return err
	}
	defer conn.terminate(ctx)
	uri := protocol.URIFromPath(args[0])
	file, err := conn.openFile(ctx, uri)
	if err != nil {
		return err
	}

	lines := bytes.Split(file.mapper.Content, []byte{'\n'})
	p := &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
		Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 0},
			End: protocol.Position{
				Line:      uint32(len(lines) - 1),
				Character: uint32(len(lines[len(lines)-1]))},
		},
	}
	resp, err := conn.semanticTokens(ctx, p)
	if err != nil {
		return err
	}
	return decorate(file, resp.Data)
}

type mark struct {
	line, offset int // 1-based, from RangeSpan
	len          int // bytes, not runes
	typ          string
	mods         []string
}

// prefixes for semantic token comments
const (
	SemanticLeft  = "/*⇐"
	SemanticRight = "/*⇒"
)

func markLine(m mark, lines [][]byte) {
	l := lines[m.line-1] // mx is 1-based
	length := utf8.RuneCount(l[m.offset-1 : m.offset-1+m.len])
	splitAt := m.offset - 1
	insert := ""
	if m.typ == "namespace" && m.offset-1+m.len < len(l) && l[m.offset-1+m.len] == '"' {
		// it is the last component of an import spec
		// cannot put a comment inside a string
		insert = fmt.Sprintf("%s%d,namespace,[]*/", SemanticLeft, length)
		splitAt = m.offset + m.len
	} else {
		// be careful not to generate //*
		spacer := ""
		if splitAt-1 >= 0 && l[splitAt-1] == '/' {
			spacer = " "
		}
		insert = fmt.Sprintf("%s%s%d,%s,%v*/", spacer, SemanticRight, length, m.typ, m.mods)
	}
	x := append([]byte(insert), l[splitAt:]...)
	l = append(l[:splitAt], x...)
	lines[m.line-1] = l
}

func decorate(file *cmdFile, result []uint32) error {
	marks := newMarks(file, result)
	if len(marks) == 0 {
		return nil
	}
	lines := bytes.Split(file.mapper.Content, []byte{'\n'})
	for i := len(marks) - 1; i >= 0; i-- {
		mx := marks[i]
		markLine(mx, lines)
	}
	os.Stdout.Write(bytes.Join(lines, []byte{'\n'}))
	return nil
}

func newMarks(file *cmdFile, d []uint32) []mark {
	ans := []mark{}
	// the following two loops could be merged, at the cost
	// of making the logic slightly more complicated to understand
	// first, convert from deltas to absolute, in LSP coordinates
	lspLine := make([]uint32, len(d)/5)
	lspChar := make([]uint32, len(d)/5)
	var line, char uint32
	for i := 0; 5*i < len(d); i++ {
		lspLine[i] = line + d[5*i+0]
		if d[5*i+0] > 0 {
			char = 0
		}
		lspChar[i] = char + d[5*i+1]
		char = lspChar[i]
		line = lspLine[i]
	}
	// second, convert to gopls coordinates
	for i := 0; 5*i < len(d); i++ {
		pr := protocol.Range{
			Start: protocol.Position{
				Line:      lspLine[i],
				Character: lspChar[i],
			},
			End: protocol.Position{
				Line:      lspLine[i],
				Character: lspChar[i] + d[5*i+2],
			},
		}
		spn, err := file.rangeSpan(pr)
		if err != nil {
			log.Fatal(err)
		}
		m := mark{
			line:   spn.Start().Line(),
			offset: spn.Start().Column(),
			len:    spn.End().Column() - spn.Start().Column(),
			typ:    protocol.SemType(int(d[5*i+3])),
			mods:   protocol.SemMods(int(d[5*i+4])),
		}
		ans = append(ans, m)
	}
	return ans
}
