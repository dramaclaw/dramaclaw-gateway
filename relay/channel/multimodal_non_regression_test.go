package channel

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Image, video and audio requests reach a provider through paths BrainClaw
// never looks at. What it does add runs on all of them: identity headers are
// stripped from every upstream request, whatever the content type.
//
// These are transport-level on purpose. Proving a picture comes back would
// need a real provider; proving the bytes and headers are untouched is what
// actually decides whether these paths regressed, and it can be shown exactly.

func TestAMultipartBodyIsForwardedByteForByte(t *testing.T) {
	var original bytes.Buffer
	writer := multipart.NewWriter(&original)
	_ = writer.WriteField("model", "gpt-image-1")
	part, _ := writer.CreateFormFile("image", "frame.png")
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\n binary payload with \x00 bytes"))
	_ = writer.Close()

	body := original.Bytes()
	request := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/images/edits",
		bytes.NewReader(body))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer sk-channel-upstream")

	stripBrainClawIdentityHeaders(request)

	forwarded, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("reading the forwarded body: %v", err)
	}
	if !bytes.Equal(forwarded, body) {
		t.Fatalf("the multipart body changed: %d bytes in, %d out", len(body), len(forwarded))
	}
	if request.Header.Get("Content-Type") != writer.FormDataContentType() {
		t.Fatalf("the multipart boundary changed, which would corrupt the body")
	}
	if request.Header.Get("Authorization") != "Bearer sk-channel-upstream" {
		t.Fatalf("the channel's upstream credential must be untouched")
	}
}

func TestIdentityHeadersNeverReachAProviderOnAnyContentType(t *testing.T) {
	for _, contentType := range []string{
		"application/json",
		"multipart/form-data; boundary=abc123",
		"application/octet-stream",
		"video/mp4",
	} {
		request := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/x", nil)
		request.Header.Set("Content-Type", contentType)
		request.Header.Set(BrainClawCapabilityHeader, "v1.k.p.s")
		request.Header.Set(BrainClawControlContextHeader, "v1.k.p.s")
		request.Header.Set("Authorization", "Bearer sk-channel-upstream")

		stripBrainClawIdentityHeaders(request)

		if request.Header.Get(BrainClawCapabilityHeader) != "" {
			t.Fatalf("%s: the capability header reached the provider", contentType)
		}
		if request.Header.Get(BrainClawControlContextHeader) != "" {
			t.Fatalf("%s: the control context header reached the provider", contentType)
		}
		if request.Header.Get("Authorization") != "Bearer sk-channel-upstream" {
			t.Fatalf("%s: stripping identity disturbed the channel credential", contentType)
		}
		if request.Header.Get("Content-Type") != contentType {
			t.Fatalf("%s: content type was altered", contentType)
		}
	}
}

func TestBusinessHeadersSurviveStripping(t *testing.T) {
	// The strip must be exact. Removing by prefix would take provider headers
	// with it, and the paths that carry the most custom headers — image and
	// video — are the ones least likely to be exercised before release.
	request := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/video", nil)
	business := map[string]string{
		"X-Request-Id":       "req-1",
		"X-Provider-Feature": "video-hd",
		"X-Stainless-Lang":   "go",
		"OpenAI-Beta":        "assistants=v2",
	}
	for name, value := range business {
		request.Header.Set(name, value)
	}
	request.Header.Set(BrainClawCapabilityHeader, "v1.k.p.s")

	stripBrainClawIdentityHeaders(request)

	for name, value := range business {
		if request.Header.Get(name) != value {
			t.Fatalf("%s was removed or altered; only the two identity headers may go", name)
		}
	}
}

func TestOnlyTheJSONPathSigns(t *testing.T) {
	// Asserted on the source because the difference is structural, and the two
	// functions are otherwise near-identical: it would be easy to "fix" the
	// asymmetry by adding a signing call to the form path, which would sign a
	// multipart body the payload cannot bind and produce an attestation that
	// verifies nothing.
	source, err := readSource("api_request.go")
	if err != nil {
		t.Fatalf("reading api_request.go: %v", err)
	}
	jsonPath := between(source, "func DoApiRequest(", "func DoFormRequest(")
	formPath := between(source, "func DoFormRequest(", "func DoWssRequest(")

	if !containsCall(jsonPath, "signBrainClawControlContext") {
		t.Fatalf("the JSON path must sign; otherwise no request is ever attested")
	}
	if containsCall(formPath, "signBrainClawControlContext") {
		t.Fatalf("the form path must not sign: a multipart body is not what the " +
			"payload binds, so the attestation would verify nothing")
	}
	for name, path := range map[string]string{"json": jsonPath, "form": formPath} {
		if !containsCall(path, "stripBrainClawIdentityHeaders") {
			t.Fatalf("the %s path must strip identity headers before sending", name)
		}
	}
}

func readSource(name string) (string, error) {
	content, err := os.ReadFile(name)
	return string(content), err
}

func between(text, start, end string) string {
	from := strings.Index(text, start)
	to := strings.Index(text, end)
	if from < 0 || to < 0 || to < from {
		return ""
	}
	return text[from:to]
}

func containsCall(text, name string) bool {
	return strings.Contains(text, name+"(")
}
