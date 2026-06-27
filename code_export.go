package main

import "loov.dev/lensm/internal/disasm"

type LineRangeDTO struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type FunctionCodeDTO struct {
	Binary    string          `json:"binary,omitempty"`
	Name      string          `json:"name"`
	File      string          `json:"file,omitempty"`
	Source    []SourceFileDTO `json:"source"`
	GoAsm     []AsmLineDTO    `json:"go_asm"`
	NativeAsm []AsmLineDTO    `json:"native_asm"`
	Comments  []CommentRecord `json:"comments,omitempty"`
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
	File    string         `json:"file"`
	Line    int            `json:"line"`
	Text    string         `json:"text"`
	Related []LineRangeDTO `json:"related,omitempty"`
	Comment string         `json:"comment,omitempty"`
}

type AsmLineDTO struct {
	Index     int    `json:"index"`
	PC        uint64 `json:"pc"`
	PCHex     string `json:"pc_hex"`
	Text      string `json:"text"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Call      string `json:"call,omitempty"`
	RefPC     uint64 `json:"ref_pc,omitempty"`
	RefPCHex  string `json:"ref_pc_hex,omitempty"`
	RefOffset int    `json:"ref_offset,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

func BuildFunctionCodeDTO(binary string, code *disasm.Code, comments *CommentStore) FunctionCodeDTO {
	if code == nil {
		return FunctionCodeDTO{Binary: cleanPath(binary)}
	}

	dto := FunctionCodeDTO{
		Binary:   cleanPath(binary),
		Name:     code.Name,
		File:     code.File,
		Comments: comments.Filter(code.Name, ""),
	}
	for _, src := range code.Source {
		srcDTO := SourceFileDTO{File: src.File}
		for _, block := range src.Blocks {
			blockDTO := SourceBlockDTO{
				From: block.From,
				To:   block.To,
			}
			for off, text := range block.Lines {
				line := block.From + off
				lineDTO := SourceLineDTO{
					File: src.File,
					Line: line,
					Text: text,
				}
				if off < len(block.Related) {
					lineDTO.Related = lineRangesDTO(block.Related[off])
				}
				if comments != nil {
					lineDTO.Comment = comments.ForSource(code.Name, src.File, line)
				}
				blockDTO.Lines = append(blockDTO.Lines, lineDTO)
			}
			srcDTO.Blocks = append(srcDTO.Blocks, blockDTO)
		}
		dto.Source = append(dto.Source, srcDTO)
	}

	for i, inst := range code.Insts {
		goLine := asmLineDTO(i, inst, inst.Text)
		nativeLine := asmLineDTO(i, inst, inst.NativeText)
		if comments != nil && inst.Text != "" {
			goLine.Comment = comments.ForAsm(code.Name, CommentViewGoAsm, inst.PC)
		}
		if comments != nil && inst.NativeText != "" {
			nativeLine.Comment = comments.ForAsm(code.Name, CommentViewNativeAsm, inst.PC)
		}
		dto.GoAsm = append(dto.GoAsm, goLine)
		dto.NativeAsm = append(dto.NativeAsm, nativeLine)
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

func asmLineDTO(index int, inst disasm.Inst, text string) AsmLineDTO {
	line := AsmLineDTO{
		Index:     index,
		PC:        inst.PC,
		PCHex:     formatPC(inst.PC),
		Text:      text,
		File:      inst.File,
		Line:      inst.Line,
		Call:      inst.Call,
		RefPC:     inst.RefPC,
		RefOffset: inst.RefOffset,
	}
	if inst.RefPC != 0 {
		line.RefPCHex = formatPC(inst.RefPC)
	}
	return line
}

func sourceLineExists(code *disasm.Code, file string, line int) bool {
	if code == nil || file == "" || line <= 0 {
		return false
	}
	for _, src := range code.Source {
		if src.File != file && cleanPath(src.File) != cleanPath(file) {
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

func asmPCExists(code *disasm.Code, view CommentView, pc uint64) bool {
	if code == nil {
		return false
	}
	for _, inst := range code.Insts {
		switch view {
		case CommentViewGoAsm:
			if inst.PC == pc && inst.Text != "" {
				return true
			}
		case CommentViewNativeAsm:
			if inst.PC == pc && inst.NativeText != "" {
				return true
			}
		}
	}
	return false
}
