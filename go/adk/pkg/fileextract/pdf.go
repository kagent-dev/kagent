package fileextract

import (
	"fmt"
	"strings"

	"github.com/tsawler/tabula"
	"github.com/tsawler/tabula/core"
	"github.com/tsawler/tabula/font"
	"github.com/tsawler/tabula/reader"
	"github.com/tsawler/tabula/text"
)

// extractPDF extracts text from a PDF file.
//
// Tabula's markdown path produces the richest output for well-behaved PDFs, but
// it has two gaps: (1) it never applies a font's ToUnicode CMap for Type3 fonts,
// so text drawn with Type3 fonts decodes to raw character codes — garbled output
// like "4; HE. HKI J" instead of "Zero Trust"; and (2) it can fail outright on
// some malformed content streams. For PDFs that use Type3 fonts, and as a
// fallback when tabula's markdown extraction fails, a tolerant per-page
// extractor (extractPDFText) is used instead.
func extractPDF(path string) (string, error) {
	if !pdfUsesType3Fonts(path) {
		if md, _, err := tabula.Open(path).ToMarkdown(); err == nil && strings.TrimSpace(md) != "" {
			return md, nil
		}
		// tabula failed or returned nothing — fall back to the per-page
		// extractor, which tolerates streams tabula can't parse.
	}

	text, err := extractPDFText(path)
	if err != nil {
		return "", fmt.Errorf("failed to extract text from pdf document: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("failed to extract text from pdf document: no extractable text")
	}
	return text, nil
}

// extractPDFText extracts text page by page using tabula's lower-level reader
// and text extractor, additionally registering Type3 fonts with their ToUnicode
// CMaps (which tabula's high-level path skips). Pages or content streams that
// fail to parse are skipped rather than aborting the whole document.
func extractPDFText(path string) (string, error) {
	r, err := reader.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}
	defer r.Close()

	pageCount, err := r.PageCount()
	if err != nil {
		return "", fmt.Errorf("failed to read PDF page count: %w", err)
	}

	resolver := func(ref core.IndirectRef) (core.Object, error) {
		return r.ResolveReference(ref)
	}

	var sb strings.Builder
	for i := range pageCount {
		page, err := r.GetPage(i)
		if err != nil || page == nil {
			continue
		}

		ex := text.NewExtractor()
		resources, _ := page.Resources()
		if resources != nil {
			// Register the font subtypes tabula handles natively, then fill
			// the Type3 gap so those fonts decode via their ToUnicode CMaps.
			_ = ex.RegisterFontsFromResources(resources, resolver)
			registerType3Fonts(ex, resources, resolver)
			ex.SetResourceContext(resources, resolver)
		}

		contents, _ := page.Contents()
		var data []byte
		for _, c := range contents {
			obj, _ := resolveObject(c, resolver)
			stream, ok := obj.(*core.Stream)
			if !ok {
				continue
			}
			decoded, err := stream.Decode()
			if err != nil {
				continue
			}
			data = append(data, decoded...)
			data = append(data, '\n')
		}
		if len(data) == 0 {
			continue
		}
		if _, err := ex.ExtractFromBytes(data); err != nil {
			continue
		}
		sb.WriteString(ex.GetText())
		sb.WriteString("\n\n")
	}

	return sb.String(), nil
}

// registerType3Fonts registers every Type3 font in a resources dictionary with
// its ToUnicode CMap so it decodes to the correct Unicode text.
func registerType3Fonts(ex *text.Extractor, resources core.Dict, resolver func(core.IndirectRef) (core.Object, error)) {
	fontDictObj, _ := resolveObject(resources.Get("Font"), resolver)
	fonts, ok := fontDictObj.(core.Dict)
	if !ok {
		return
	}

	for name, fontObj := range fonts {
		resolved, _ := resolveObject(fontObj, resolver)
		fontDict, ok := resolved.(core.Dict)
		if !ok {
			continue
		}
		if subtype, _ := fontDict.GetName("Subtype"); string(subtype) != "Type3" {
			continue
		}

		f := font.NewFont(name, "", "Type3")
		if tu := fontDict.Get("ToUnicode"); tu != nil {
			if obj, _ := resolveObject(tu, resolver); obj != nil {
				if stream, ok := obj.(*core.Stream); ok {
					if cmap, err := font.ParseToUnicodeCMap(stream); err == nil {
						f.ToUnicodeCMap = cmap
					}
				}
			}
		}

		ex.RegisterParsedFont(name, f)
		if !strings.HasPrefix(name, "/") {
			ex.RegisterParsedFont("/"+name, f)
		}
	}
}

// pdfUsesType3Fonts reports whether any page resource references a Type3 font.
// It scans font dictionaries only (no text extraction), so it is cheap.
func pdfUsesType3Fonts(path string) bool {
	r, err := reader.Open(path)
	if err != nil {
		return false
	}
	defer r.Close()

	pageCount, err := r.PageCount()
	if err != nil {
		return false
	}
	resolver := func(ref core.IndirectRef) (core.Object, error) {
		return r.ResolveReference(ref)
	}

	for i := range pageCount {
		page, err := r.GetPage(i)
		if err != nil || page == nil {
			continue
		}
		resources, _ := page.Resources()
		if resources == nil {
			continue
		}
		fontDictObj, _ := resolveObject(resources.Get("Font"), resolver)
		fonts, ok := fontDictObj.(core.Dict)
		if !ok {
			continue
		}
		for _, fontObj := range fonts {
			resolved, _ := resolveObject(fontObj, resolver)
			fontDict, ok := resolved.(core.Dict)
			if !ok {
				continue
			}
			if subtype, _ := fontDict.GetName("Subtype"); string(subtype) == "Type3" {
				return true
			}
		}
	}
	return false
}

// resolveObject dereferences an indirect reference, returning other objects
// unchanged.
func resolveObject(obj core.Object, resolver func(core.IndirectRef) (core.Object, error)) (core.Object, error) {
	if ref, ok := obj.(core.IndirectRef); ok {
		return resolver(ref)
	}
	return obj, nil
}
