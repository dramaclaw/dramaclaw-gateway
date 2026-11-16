package openai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// OpenRouter's Image API uses one JSON endpoint for generation and editing:
// https://openrouter.ai/docs/guides/overview/multimodal/image-generation
func openRouterImagePayload(c *gin.Context, request dto.ImageRequest, edit bool) (map[string]any, error) {
	if request.N != nil && (*request.N < 1 || *request.N > 10) {
		return nil, fmt.Errorf("OpenRouter image n must be between 1 and 10")
	}
	if len(request.Mask) > 0 && string(request.Mask) != "null" {
		return nil, fmt.Errorf("OpenRouter image adapter does not support OpenAI masks")
	}
	payload := map[string]any{"model": request.Model, "prompt": request.Prompt}
	size := strings.TrimSpace(request.Size)
	if strings.EqualFold(size, "auto") {
		size = ""
	}
	if request.N != nil {
		payload["n"] = *request.N
	}
	if size != "" {
		payload["size"] = size
	}
	if request.Quality != "" {
		payload["quality"] = request.Quality
	}
	if request.Stream != nil {
		payload["stream"] = *request.Stream
	}
	for key, raw := range map[string]json.RawMessage{
		"background": request.Background, "output_format": request.OutputFormat,
		"output_compression": request.OutputCompression, "user": request.User,
	} {
		if len(raw) > 0 {
			payload[key] = raw
		}
	}
	// Only documented Image API fields may bypass the OpenAI request DTO.
	// In particular, extras cannot replace the mapped model or validated n.
	for _, key := range []string{"resolution", "aspect_ratio", "seed", "provider"} {
		if raw := request.Extra[key]; len(raw) > 0 {
			payload[key] = raw
		}
	}
	ratio, _ := request.Metadata["ratio"].(string)
	ratio = strings.ToLower(strings.TrimSpace(ratio))
	if ratio == "adaptive" {
		ratio = "auto"
	}
	resolution, _ := request.Metadata["resolution"].(string)
	resolution = strings.ToUpper(strings.TrimSpace(resolution))
	// CE's pixel size is derived from its ratio/resolution selectors. Prefer
	// those selectors: not every OpenRouter image provider accepts pixel sizes.
	if size == "" || (request.Width > 0 && (ratio != "" || resolution != "")) {
		delete(payload, "size")
		if ratio != "" {
			if _, exists := payload["aspect_ratio"]; !exists {
				payload["aspect_ratio"] = ratio
			}
		}
		if resolution != "" {
			if _, exists := payload["resolution"]; !exists {
				payload["resolution"] = resolution
			}
		}
	}

	var references []any
	for _, raw := range []json.RawMessage{request.Image, request.Images, request.Extra["input_references"]} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		converted, err := openRouterImageReferences(raw)
		if err != nil {
			return nil, err
		}
		references = append(references, converted...)
	}
	if c != nil && c.Request != nil && strings.HasPrefix(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		files, err := openRouterMultipartImages(c, payload)
		if err != nil {
			return nil, err
		}
		references = append(references, files...)
	}
	if len(references) > 0 {
		payload["input_references"] = references
	} else if edit {
		return nil, fmt.Errorf("OpenRouter image edit requires at least one reference image")
	}
	return payload, nil
}

func openRouterImageReferences(raw json.RawMessage) ([]any, error) {
	var decoded any
	if err := common.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("invalid OpenRouter image references")
	}
	items, ok := decoded.([]any)
	if !ok {
		items = []any{decoded}
	}
	references := make([]any, 0, len(items))
	for _, item := range items {
		var imageURL string
		switch value := item.(type) {
		case string:
			imageURL = value
		case map[string]any:
			if kind, exists := value["type"]; exists && kind != "image_url" {
				return nil, fmt.Errorf("OpenRouter image references must have type image_url")
			}
			switch image := value["image_url"].(type) {
			case string:
				imageURL = image
			case map[string]any:
				imageURL, _ = image["url"].(string)
			default:
				imageURL, _ = value["url"].(string)
			}
		}
		imageURL = strings.TrimSpace(imageURL)
		parsed, err := url.Parse(imageURL)
		isHTTP := err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
		isData := strings.HasPrefix(imageURL, "data:image/") && strings.Contains(imageURL, ",")
		if !isHTTP && !isData {
			return nil, fmt.Errorf("OpenRouter references require an HTTP(S) image URL or image data URL")
		}
		references = append(references, map[string]any{
			"type": "image_url", "image_url": map[string]string{"url": imageURL},
		})
	}
	return references, nil
}

func openRouterMultipartImages(c *gin.Context, payload map[string]any) ([]any, error) {
	form := c.Request.MultipartForm
	if form == nil {
		var err error
		form, err = common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, fmt.Errorf("failed to parse OpenRouter image form")
		}
	}
	if len(form.File["mask"]) > 0 || len(form.Value["mask"]) > 0 {
		return nil, fmt.Errorf("OpenRouter image adapter does not support OpenAI masks")
	}
	for _, key := range []string{"output_format", "background", "user", "resolution", "aspect_ratio"} {
		if values := form.Value[key]; len(values) > 0 {
			payload[key] = values[0]
		}
	}
	for _, key := range []string{"seed", "output_compression", "provider"} {
		if values := form.Value[key]; len(values) > 0 {
			var value any
			if err := common.Unmarshal([]byte(values[0]), &value); err != nil {
				return nil, fmt.Errorf("invalid OpenRouter image form field %s", key)
			}
			payload[key] = json.RawMessage(values[0])
		}
	}
	keys := make([]string, 0, len(form.File))
	for key := range form.File {
		if key == "image" || key == "image[]" || (strings.HasPrefix(key, "image[") && strings.HasSuffix(key, "]")) {
			keys = append(keys, key)
		}
	}
	// Indexed image fields express reference order; map iteration must not
	// decide which image becomes the primary reference.
	sort.Slice(keys, func(i, j int) bool {
		a, aErr := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(keys[i], "image["), "]"))
		b, bErr := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(keys[j], "image["), "]"))
		if aErr == nil && bErr == nil && a != b {
			return a < b
		}
		if (aErr == nil) != (bErr == nil) {
			return aErr != nil
		}
		return keys[i] < keys[j]
	})
	var references []any
	for _, key := range keys {
		for _, header := range form.File[key] {
			file, err := header.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open OpenRouter reference image")
			}
			data, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read OpenRouter reference image")
			}
			mimeType := http.DetectContentType(data)
			if !strings.HasPrefix(mimeType, "image/") {
				mimeType = detectImageMimeType(header.Filename)
			}
			references = append(references, map[string]any{
				"type": "image_url", "image_url": map[string]string{
					"url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
				},
			})
		}
	}
	return references, nil
}
