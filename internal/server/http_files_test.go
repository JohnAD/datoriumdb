package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

const testFileDocID = "01FILETEST00000000000001"

func postJSONCommand(t *testing.T, baseURL, token string, cmd map[string]any) *http.Response {
	t.Helper()
	body, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/datoriumdb/v1/command", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func postMultipartFileCommand(t *testing.T, baseURL, token string, cmd map[string]any, contentType string, content []byte) *http.Response {
	t.Helper()
	cmdJSON, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	cmdPart, err := mw.CreateFormField("command")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cmdPart.Write(cmdJSON); err != nil {
		t.Fatal(err)
	}
	partHdr := textproto.MIMEHeader{}
	partHdr.Set("Content-Disposition", `form-data; name="content"; filename="blob"`)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	partHdr.Set("Content-Type", contentType)
	contentPart, err := mw.CreatePart(partHdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentPart.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/datoriumdb/v1/command", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestFileHTTPLifecycle(t *testing.T) {
	ts, eng, issuer := testHarness(t)
	tok, _, err := issuer.IssueClientToken("alice", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Create parent document via JSON command.
	createResp := postJSONCommand(t, ts.URL, tok, map[string]any{
		"command": "create", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{"$": "Movies:0", "title": "T", "releaseYear": 1999},
	})
	createEnv := decodeEnvelope(t, createResp)
	if createEnv["ok"] != true {
		t.Fatalf("create failed: %#v", createEnv)
	}

	payload := []byte("png-bytes-here")
	putResp := postMultipartFileCommand(t, ts.URL, tok, map[string]any{
		"command": "fileCreate", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{"filename": "photo.png", "contentType": "image/png"},
	}, "image/png", payload)
	putEnv := decodeEnvelope(t, putResp)
	if putEnv["ok"] != true {
		t.Fatalf("put failed: %#v", putEnv)
	}
	if putEnv["command"] != "fileCreate" {
		t.Fatalf("command=%v", putEnv["command"])
	}
	version, _ := putEnv["version"].(string)
	if version == "" {
		t.Fatal("missing version")
	}

	// Duplicate create without version → fileExists
	dupResp := postMultipartFileCommand(t, ts.URL, tok, map[string]any{
		"command": "fileCreate", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{"filename": "photo.png", "contentType": "image/png"},
	}, "image/png", payload)
	dupEnv := decodeEnvelope(t, dupResp)
	if dupEnv["ok"] != false || firstErrCode(t, dupEnv) != "fileExists" {
		t.Fatalf("expected fileExists, got %#v", dupEnv)
	}

	readResp := postJSONCommand(t, ts.URL, tok, map[string]any{
		"command": "fileRead", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{"filename": "photo.png"},
	})
	defer readResp.Body.Close()
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("fileRead status %d", readResp.StatusCode)
	}
	if readResp.Header.Get("X-DatoriumDB-File-Version") != version {
		t.Fatalf("version header %q", readResp.Header.Get("X-DatoriumDB-File-Version"))
	}
	if readResp.Header.Get("X-DatoriumDB-SHA256") == "" {
		t.Fatal("missing X-DatoriumDB-SHA256")
	}
	got, _ := io.ReadAll(readResp.Body)
	if !bytes.Equal(got, payload) {
		t.Fatalf("download = %q", got)
	}

	listResp := postJSONCommand(t, ts.URL, tok, map[string]any{
		"command": "fileList", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{},
	})
	listEnv := decodeEnvelope(t, listResp)
	files, _ := listEnv["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("list %#v", listEnv)
	}

	updated := []byte("updated-bytes")
	updResp := postMultipartFileCommand(t, ts.URL, tok, map[string]any{
		"command": "fileUpdate", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{"filename": "photo.png", "contentType": "image/png", "version": version},
	}, "image/png", updated)
	updEnv := decodeEnvelope(t, updResp)
	if updEnv["ok"] != true || updEnv["command"] != "fileUpdate" {
		t.Fatalf("update %#v", updEnv)
	}
	newVer, _ := updEnv["version"].(string)

	delResp := postJSONCommand(t, ts.URL, tok, map[string]any{
		"command": "fileDelete", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{"filename": "photo.png", "version": newVer},
	})
	delEnv := decodeEnvelope(t, delResp)
	if delEnv["ok"] != true {
		t.Fatalf("delete %#v", delEnv)
	}

	entries, _ := fsstore.ReadFilesManifest(eng.DataDir, "Movies", testFileDocID)
	if len(entries) != 0 {
		t.Fatalf("manifest should be empty: %#v", entries)
	}
}

func TestFileHTTPInvalidFileName(t *testing.T) {
	ts, _, issuer := testHarness(t)
	tok, _, err := issuer.IssueClientToken("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	resp := postMultipartFileCommand(t, ts.URL, tok, map[string]any{
		"command": "fileCreate", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{"filename": ".hidden", "contentType": "application/octet-stream"},
	}, "application/octet-stream", []byte("x"))
	env := decodeEnvelope(t, resp)
	if env["ok"] != false || firstErrCode(t, env) != "invalidFileName" {
		t.Fatalf("expected invalidFileName, got %#v", env)
	}
}

func TestFileHTTPLargeUploadAndReadStream(t *testing.T) {
	ts, _, issuer := testHarness(t)
	tok, _, err := issuer.IssueClientToken("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	docID := "01FILETEST00000000000002"
	createResp := postJSONCommand(t, ts.URL, tok, map[string]any{
		"command": "create", "target": "Movies", "parameter": docID,
		"detail": map[string]any{"$": "Movies:0", "title": "Big", "releaseYear": 2000},
	})
	if env := decodeEnvelope(t, createResp); env["ok"] != true {
		t.Fatalf("create %#v", env)
	}

	// Larger than the JSON response cap (8 MiB) would be ideal; use a few MiB
	// to prove multipart content is accepted and fileRead streams raw bytes.
	payload := bytes.Repeat([]byte("abcdefgh"), 256*1024) // 2 MiB
	putResp := postMultipartFileCommand(t, ts.URL, tok, map[string]any{
		"command": "fileCreate", "target": "Movies", "parameter": docID,
		"detail": map[string]any{"filename": "big.bin", "contentType": "application/octet-stream"},
	}, "application/octet-stream", payload)
	putEnv := decodeEnvelope(t, putResp)
	if putEnv["ok"] != true {
		t.Fatalf("put %#v", putEnv)
	}
	if int64(putEnv["byteSize"].(float64)) != int64(len(payload)) {
		t.Fatalf("byteSize=%v want %d", putEnv["byteSize"], len(payload))
	}

	readResp := postJSONCommand(t, ts.URL, tok, map[string]any{
		"command": "fileRead", "target": "Movies", "parameter": docID,
		"detail": map[string]any{"filename": "big.bin"},
	})
	defer readResp.Body.Close()
	got, err := io.ReadAll(readResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(payload))
	}
	if readResp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("ct=%q", readResp.Header.Get("Content-Type"))
	}
}
