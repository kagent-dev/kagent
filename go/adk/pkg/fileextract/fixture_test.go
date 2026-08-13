package fileextract

import (
	"bytes"
	"fmt"
)

// type3PDFFixture builds a minimal, valid PDF whose single page draws text with
// a Type3 font. The font has no embedded glyphs that map to Unicode by encoding;
// the only correct source of text is its ToUnicode CMap (code 0x01->'H',
// 0x02->'i'). The content stream shows <0102>, so a ToUnicode-aware extractor
// yields "Hi" while a naive one yields the raw code bytes.
func type3PDFFixture() []byte {
	objects := []string{
		// 1: Catalog
		"<< /Type /Catalog /Pages 2 0 R >>",
		// 2: Pages
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// 3: Page
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		// 4: Type3 font
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] " +
			"/FontMatrix [0.001 0 0 0.001 0 0] /FirstChar 1 /LastChar 2 " +
			"/Widths [500 500] /CharProcs << /a1 6 0 R /a2 7 0 R >> " +
			"/Encoding << /Type /Encoding /Differences [1 /a1 /a2] >> " +
			"/ToUnicode 8 0 R >>",
		// 5: page content stream
		streamObject("BT\n/F1 12 Tf\n10 100 Td\n<0102> Tj\nET\n"),
		// 6: CharProc for code 1
		streamObject("500 0 0 0 500 500 d1\n"),
		// 7: CharProc for code 2
		streamObject("500 0 0 0 500 500 d1\n"),
		// 8: ToUnicode CMap
		streamObject(toUnicodeCMap()),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, xrefStart)

	return buf.Bytes()
}

func streamObject(content string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
}

func toUnicodeCMap() string {
	return "/CIDInit /ProcSet findresource begin\n" +
		"12 dict begin\nbegincmap\n" +
		"/CMapName /Adobe-Identity-UCS def\n" +
		"/CMapType 2 def\n" +
		"1 begincodespacerange\n<00> <FF>\nendcodespacerange\n" +
		"2 beginbfchar\n<01> <0048>\n<02> <0069>\nendbfchar\n" +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
}
