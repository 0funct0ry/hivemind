package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadAPI(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	// 1. Test multipart upload (txt file)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello text file"))
	writer.Close()

	req, _ := http.NewRequest("POST", "/api/v1/uploads", body)
	req.Host = "example.com"
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(&http.Cookie{Name: "hm_session", Value: tc.sMember})
	w := httptest.NewRecorder()
	tc.r.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d. Body: %s", w.Code, w.Body.String())
	}

	var uploadResp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Mime string `json:"mime"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &uploadResp); err != nil {
		t.Fatal(err)
	}

	if uploadResp.Name != "test.txt" {
		t.Errorf("expected name test.txt, got %s", uploadResp.Name)
	}

	// 2. Test downloading the uploaded file (must be served as attachment)
	reqGet, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/files/%s/test.txt", uploadResp.ID), nil)
	reqGet.Host = "example.com"
	reqGet.AddCookie(&http.Cookie{Name: "hm_session", Value: tc.sMember})
	wGet := httptest.NewRecorder()
	tc.r.ServeHTTP(wGet, reqGet)

	if wGet.Code != 200 {
		t.Fatalf("expected 200, got %d", wGet.Code)
	}
	if wGet.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected nosniff, got %s", wGet.Header().Get("X-Content-Type-Options"))
	}
	if !strings.Contains(wGet.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("expected attachment Content-Disposition, got %s", wGet.Header().Get("Content-Disposition"))
	}

	// 3. Test uploading PNG image
	pngBody := &bytes.Buffer{}
	pngWriter := multipart.NewWriter(pngBody)
	// minimal 1x1 pixel PNG
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x42, 0x54, 0x78, 0xDA, 0x63, 0x60, 0x18, 0x05, 0xA3,
		0x60, 0x14, 0x8C, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	}
	pngPart, _ := pngWriter.CreateFormFile("file", "image.png")
	_, _ = pngPart.Write(pngData)
	pngWriter.Close()

	reqPng, _ := http.NewRequest("POST", "/api/v1/uploads", pngBody)
	reqPng.Host = "example.com"
	reqPng.Header.Set("Content-Type", pngWriter.FormDataContentType())
	reqPng.Header.Set("Origin", "http://example.com")
	reqPng.AddCookie(&http.Cookie{Name: "hm_session", Value: tc.sMember})
	wPng := httptest.NewRecorder()
	tc.r.ServeHTTP(wPng, reqPng)

	if wPng.Code != 201 {
		t.Fatalf("expected 201, got %d", wPng.Code)
	}

	var pngResp struct {
		ID     string `json:"id"`
		Mime   string `json:"mime"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.Unmarshal(wPng.Body.Bytes(), &pngResp); err != nil {
		t.Fatal(err)
	}
	if pngResp.Width != 1 || pngResp.Height != 1 {
		t.Errorf("expected dimensions 1x1, got %dx%d", pngResp.Width, pngResp.Height)
	}

	// 4. Test downloading PNG image (must be served as inline)
	reqPngGet, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/files/%s/image.png", pngResp.ID), nil)
	reqPngGet.Host = "example.com"
	reqPngGet.AddCookie(&http.Cookie{Name: "hm_session", Value: tc.sMember})
	wPngGet := httptest.NewRecorder()
	tc.r.ServeHTTP(wPngGet, reqPngGet)

	if wPngGet.Code != 200 {
		t.Fatalf("expected 200, got %d", wPngGet.Code)
	}
	if !strings.Contains(wPngGet.Header().Get("Content-Disposition"), "inline") {
		t.Errorf("expected inline Content-Disposition, got %s", wPngGet.Header().Get("Content-Disposition"))
	}

	// 5. Test uploading SVG (must be forced to download as attachment)
	svgBody := &bytes.Buffer{}
	svgWriter := multipart.NewWriter(svgBody)
	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	svgPart, _ := svgWriter.CreateFormFile("file", "evil.svg")
	_, _ = svgPart.Write(svgData)
	svgWriter.Close()

	reqSvg, _ := http.NewRequest("POST", "/api/v1/uploads", svgBody)
	reqSvg.Host = "example.com"
	reqSvg.Header.Set("Content-Type", svgWriter.FormDataContentType())
	reqSvg.Header.Set("Origin", "http://example.com")
	reqSvg.AddCookie(&http.Cookie{Name: "hm_session", Value: tc.sMember})
	wSvg := httptest.NewRecorder()
	tc.r.ServeHTTP(wSvg, reqSvg)

	if wSvg.Code != 201 {
		t.Fatalf("expected 201, got %d", wSvg.Code)
	}

	var svgResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(wSvg.Body.Bytes(), &svgResp)

	reqSvgGet, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/files/%s/evil.svg", svgResp.ID), nil)
	reqSvgGet.AddCookie(&http.Cookie{Name: "hm_session", Value: tc.sMember})
	wSvgGet := httptest.NewRecorder()
	tc.r.ServeHTTP(wSvgGet, reqSvgGet)

	if wSvgGet.Code != 200 {
		t.Fatalf("expected 200, got %d", wSvgGet.Code)
	}
	if !strings.Contains(wSvgGet.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("expected SVG to download as attachment, got %s", wSvgGet.Header().Get("Content-Disposition"))
	}

	// 6. Test posting message with this attachment
	msgBody := map[string]any{
		"body":     "Check out this text file!",
		"file_ids": []string{uploadResp.ID},
	}
	code, respMsg := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, msgBody)
	if code != 201 {
		t.Fatalf("expected 201 posting message with attachment, got %d. Body: %s", code, respMsg)
	}

	var msgResp struct {
		Message struct {
			ID             string `json:"id"`
			HasAttachments bool   `json:"has_attachments"`
			Attachments    []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"attachments"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(respMsg), &msgResp); err != nil {
		t.Fatal(err)
	}

	if !msgResp.Message.HasAttachments {
		t.Error("expected message.has_attachments to be true")
	}
	if len(msgResp.Message.Attachments) != 1 || msgResp.Message.Attachments[0].ID != uploadResp.ID {
		t.Errorf("expected 1 attachment with ID %s, got %+v", uploadResp.ID, msgResp.Message.Attachments)
	}

	// 7. Test posting message with attachment uploaded by another user -> 403 Forbidden
	msgBodyForbidden := map[string]any{
		"body":     "Trying to steal an attachment!",
		"file_ids": []string{uploadResp.ID}, // uploadResp was uploaded by tc.sMember, now posting as tc.sAdmin
	}
	codeForbidden, _ := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sAdmin, msgBodyForbidden)
	if codeForbidden != 403 {
		t.Errorf("expected 403 Forbidden for using another user's attachment, got %d", codeForbidden)
	}
}
