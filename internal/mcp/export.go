package mcp

import (
	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
)

type LineRangeDTO struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type FunctionCodeDTO struct {
	Binary string          `json:"binary,omitempty"`
	Name   string          `json:"name"`
	File   string          `json:"file,omitempty"`
	Arch   string          `json:"arch,omitempty"`
	Source []SourceFileDTO `json:"source,omitempty"`
	// Asm is one page of instructions, from AsmOffset; AsmTotal counts
	// all of them and Truncated reports more after this page.
	Asm       []AsmLineDTO `json:"asm,omitempty"`
	AsmOffset int          `json:"asm_offset"`
	AsmTotal  int          `json:"asm_total"`
	Truncated bool         `json:"truncated,omitempty"`
}

type SourceFileDTO struct {
	File   string           `json:"file"`
	Blocks []SourceBlockDTO `json:"blocks"`
}

type SourceBlockDTO struct {
	From  int             `json:"from"`
	To    int             `json:"to"`
	Lines []SourceLineDTO `json:"lines"`
}

type SourceLineDTO struct {
	Line    int            `json:"line"`
	Text    string         `json:"text"`
	Related []LineRangeDTO `json:"related,omitempty"`
	Comment string         `json:"comment,omitempty"`
}

// AsmLineDTO is one instruction with both its syntaxes, so position,
// call and jump are written once. File is left out when it is the
// function's own. Index counts the spacer rows the
// viewer puts before jump targets, which RefOffset and Related use too.
type AsmLineDTO struct {
	Index         int    `json:"index"`
	PC            string `json:"pc"`
	Go            string `json:"go,omitempty"`
	Native        string `json:"native,omitempty"`
	Indent        int    `json:"indent,omitempty"`
	File          string `json:"file,omitempty"`
	Line          int    `json:"line,omitempty"`
	Call          string `json:"call,omitempty"`
	RefPC         string `json:"ref_pc,omitempty"`
	RefOffset     int    `json:"ref_offset,omitempty"`
	GoComment     string `json:"go_comment,omitempty"`
	NativeComment string `json:"native_comment,omitempty"`
}

// ExportOptions selects what BuildFunctionCodeDTO includes.
type ExportOptions struct {
	Source, GoAsm, NativeAsm bool
	// Offset and Limit select a page of instructions; Limit 0 means all.
	Offset, Limit int
}

func BuildFunctionCodeDTO(binary string, code *disasm.Code, store *comments.Store, opts ExportOptions) FunctionCodeDTO {
	if code == nil {
		return FunctionCodeDTO{Binary: comments.CleanPath(binary)}
	}

	dto := FunctionCodeDTO{
		Binary:   comments.CleanPath(binary),
		Name:     code.Name,
		File:     code.File,
		Arch:     code.Arch,
		AsmTotal: len(code.Insts),
	}
	if opts.Source {
		for _, src := range code.Source {
			srcDTO := SourceFileDTO{File: src.File}
			for _, block := range src.Blocks {
				blockDTO := SourceBlockDTO{
					From: block.From,
					To:   block.To,
				}
				for off, text := range block.Lines {
					line := block.From + off
					lineDTO := SourceLineDTO{Line: line, Text: text}
					if off < len(block.Related) {
						lineDTO.Related = lineRangesDTO(block.Related[off])
					}
					lineDTO.Comment = store.ForSource(code.Name, src.File, line)
					blockDTO.Lines = append(blockDTO.Lines, lineDTO)
				}
				srcDTO.Blocks = append(srcDTO.Blocks, blockDTO)
			}
			dto.Source = append(dto.Source, srcDTO)
		}
	}

	if !opts.GoAsm && !opts.NativeAsm {
		return dto
	}
	from := min(max(opts.Offset, 0), len(code.Insts))
	to := len(code.Insts)
	if opts.Limit > 0 && from+opts.Limit < to {
		to = from + opts.Limit
		dto.Truncated = true
	}
	dto.AsmOffset = from
	for i := from; i < to; i++ {
		inst := code.Insts[i]
		if inst.Text == "" && inst.NativeText == "" {
			continue // a spacer row
		}
		line := AsmLineDTO{
			Index:     i,
			PC:        comments.FormatPC(inst.PC),
			Indent:    inst.Indent,
			File:      inst.File,
			Line:      inst.Line,
			Call:      inst.Call,
			RefOffset: inst.RefOffset,
		}
		if line.File == code.File {
			line.File = ""
		}
		if inst.RefPC != 0 {
			line.RefPC = comments.FormatPC(inst.RefPC)
		}
		if opts.GoAsm {
			line.Go = inst.Text
			line.GoComment = store.ForAsm(code.Name, comments.ViewGoAsm, inst.PC)
		}
		if opts.NativeAsm {
			line.Native = inst.NativeText
			line.NativeComment = store.ForAsm(code.Name, comments.ViewNativeAsm, inst.PC)
		}
		dto.Asm = append(dto.Asm, line)
	}
	return dto
}

func lineRangesDTO(ranges []disasm.LineRange) []LineRangeDTO {
	if len(ranges) == 0 {
		return nil
	}
	out := make([]LineRangeDTO, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, LineRangeDTO{From: r.From, To: r.To})
	}
	return out
}

func sourceLineExists(code *disasm.Code, file string, line int) bool {
	if code == nil || file == "" || line <= 0 {
		return false
	}
	for _, src := range code.Source {
		if src.File != file && comments.CleanPath(src.File) != comments.CleanPath(file) {
			continue
		}
		for _, block := range src.Blocks {
			if block.From <= line && line < block.From+len(block.Lines) {
				return true
			}
		}
	}
	return false
}

func asmPCExists(code *disasm.Code, view comments.View, pc uint64) bool {
	if code == nil {
		return false
	}
	for _, inst := range code.Insts {
		switch view {
		case comments.ViewGoAsm:
			if inst.PC == pc && inst.Text != "" {
				return true
			}
		case comments.ViewNativeAsm:
			if inst.PC == pc && inst.NativeText != "" {
				return true
			}
		}
	}
	return false
}
