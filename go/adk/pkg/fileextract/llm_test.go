package fileextract

import (
	"context"
	"iter"
	"slices"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type recordingLLM struct{ got *model.LLMRequest }

func (r *recordingLLM) Name() string { return "recording" }

func (r *recordingLLM) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	r.got = req
	return func(func(*model.LLMResponse, error) bool) {}
}

func TestWithFileText(t *testing.T) {
	csv := &genai.Part{InlineData: &genai.Blob{Data: []byte("vendor,amount\nAcme,42"), MIMEType: "text/csv", DisplayName: "invoice.csv"}}
	image := &genai.Part{InlineData: &genai.Blob{Data: []byte{0x89}, MIMEType: "image/png"}}
	history := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "hi"}, csv}}
	user := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "explain"}, csv, image}}
	req := &model.LLMRequest{Contents: []*genai.Content{history, user}}

	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}

	got := inner.got.Contents
	if got[0] != history {
		t.Error("non-user content was converted or copied")
	}
	parts := got[1].Parts
	if want := "Contents of uploaded file \"invoice.csv\":\n\nvendor,amount\nAcme,42"; parts[1].Text != want || parts[1].InlineData != nil {
		t.Errorf("file part = %+v, want text %q", parts[1], want)
	}
	if parts[2] != image {
		t.Error("image part was not passed through")
	}
	if user.Parts[1] != csv || req.Contents[1] != user {
		t.Error("caller's request was mutated")
	}
}

func TestWithFileText_NoFilesPassesRequestThrough(t *testing.T) {
	dataPart := &genai.Part{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte(`<a2a_datapart_json>{"a":1}</a2a_datapart_json>`)}}
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}, dataPart}}}}
	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}
	if inner.got != req {
		t.Error("request without files (a data part is not one) was copied")
	}
}

func TestWithFileText_NilRequest(t *testing.T) {
	inner := &recordingLLM{got: &model.LLMRequest{}}
	for range WithFileText(inner).GenerateContent(t.Context(), nil, false) {
	}
	if inner.got != nil {
		t.Error("nil request was not passed through")
	}
}

type googleRecordingLLM struct{ recordingLLM }

func (googleRecordingLLM) GetGoogleLLMVariant() genai.Backend { return genai.BackendGeminiAPI }

func TestWithFileText_KeepsGoogleLLMVariant(t *testing.T) {
	g, ok := WithFileText(&googleRecordingLLM{}).(interface{ GetGoogleLLMVariant() genai.Backend })
	if !ok {
		t.Fatal("wrapper hides GetGoogleLLMVariant")
	}
	if got := g.GetGoogleLLMVariant(); got != genai.BackendGeminiAPI {
		t.Errorf("variant = %v, want %v", got, genai.BackendGeminiAPI)
	}
}

func TestWithFileText_LeavesDataPartsNextToFiles(t *testing.T) {
	dataPart := &genai.Part{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte(`<a2a_datapart_json>{"a":1}</a2a_datapart_json>`)}}
	file := &genai.Part{InlineData: &genai.Blob{MIMEType: "text/plain", DisplayName: "notes.txt", Data: []byte("<a2a_datapart_json> is just text here")}}
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{dataPart, file}}}}
	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}
	parts := inner.got.Contents[0].Parts
	if parts[0] != dataPart {
		t.Error("data part was converted")
	}
	if !strings.Contains(parts[1].Text, "is just text here") {
		t.Errorf("file was not converted: %+v", parts[1])
	}
}

// pdfSendingLLM sends pdf bytes and links itself, like the OpenAI Responses adapter.
type pdfSendingLLM struct{ recordingLLM }

func (*pdfSendingLLM) ProviderAccepts(p *genai.Part) bool {
	return p.FileData != nil || p.InlineData != nil && p.InlineData.MIMEType == "application/pdf"
}

func TestWithFileText_LeavesFilesTheModelSends(t *testing.T) {
	pdf := &genai.Part{InlineData: &genai.Blob{Data: []byte("%PDF"), MIMEType: "application/pdf", DisplayName: "report.pdf"}}
	link := &genai.Part{FileData: &genai.FileData{FileURI: "https://files.example.com/report.pdf"}}
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{pdf, link}}}}

	sender := &pdfSendingLLM{}
	for range WithFileText(sender).GenerateContent(t.Context(), req, false) {
	}
	if sender.got != req {
		t.Errorf("request with files the model sends was changed: %+v", sender.got.Contents[0].Parts)
	}

	google := &googleRecordingLLM{}
	linkOnly := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{link}}}}
	for range WithFileText(google).GenerateContent(t.Context(), linkOnly, false) {
	}
	if google.got != linkOnly {
		t.Error("Gemini link was changed")
	}

	other := &recordingLLM{}
	pdfOnly := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{pdf}}}}
	for range WithFileText(other).GenerateContent(t.Context(), pdfOnly, false) {
	}
	if got := other.got.Contents[0].Parts[0].Text; !strings.Contains(got, "could not be read") {
		t.Errorf("pdf for a model that cannot send it = %q, want the could-not-be-read note", got)
	}
}

func TestWithFileText_LinkTheModelCannotSend(t *testing.T) {
	link := &genai.Part{FileData: &genai.FileData{FileURI: "https://files.example.com/report.pdf"}}
	user := func(parts ...*genai.Part) *genai.Content { return &genai.Content{Role: genai.RoleUser, Parts: parts} }

	texts := func(c *genai.Content) []string {
		var out []string
		for _, p := range c.Parts {
			if p.FileData != nil {
				out = append(out, "<link part>")
			} else {
				out = append(out, p.Text)
			}
		}
		return out
	}

	for name, tc := range map[string]struct {
		in   *genai.Content
		want []string
	}{
		"link alone becomes its URL": {user(genai.NewPartFromText("read this"), link), []string{"read this", link.FileData.FileURI}},
		"link already in the text is dropped": {
			user(genai.NewPartFromText("read "+link.FileData.FileURI), link),
			[]string{"read " + link.FileData.FileURI},
		},
	} {
		t.Run(name, func(t *testing.T) {
			inner := &recordingLLM{}
			// The link sits in an older turn too: replayed history gets the same text.
			req := &model.LLMRequest{Contents: []*genai.Content{
				tc.in,
				{Role: genai.RoleModel, Parts: []*genai.Part{genai.NewPartFromText("ok")}},
				tc.in,
			}}
			for _, err := range WithFileText(inner).GenerateContent(t.Context(), req, false) {
				t.Fatalf("err = %v", err)
			}
			for _, i := range []int{0, 2} {
				if got := texts(inner.got.Contents[i]); !slices.Equal(got, tc.want) {
					t.Errorf("turn %d = %q, want %q", i, got, tc.want)
				}
			}
			if len(tc.in.Parts) != 2 || tc.in.Parts[1] != link {
				t.Error("the session's own content was changed")
			}
		})
	}
}

func TestWithFileText_NameFromPartMetadata(t *testing.T) {
	file := &genai.Part{
		InlineData:   &genai.Blob{Data: []byte("remember the milk"), MIMEType: "application/octet-stream"},
		PartMetadata: map[string]any{FilenameMetadataKey: "notes.txt"},
	}
	req := &model.LLMRequest{Contents: []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{file}}}}
	inner := &recordingLLM{}
	for range WithFileText(inner).GenerateContent(t.Context(), req, false) {
	}
	if want := "Contents of uploaded file \"notes.txt\":\n\nremember the milk"; inner.got.Contents[0].Parts[0].Text != want {
		t.Errorf("file part = %+v, want text %q", inner.got.Contents[0].Parts[0], want)
	}
}
