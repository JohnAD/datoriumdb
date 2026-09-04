package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
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

func postJSONCommandWithRange(t *testing.T, baseURL, token string, cmd map[string]any, byteRange string) *http.Response {
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
	req.Header.Set("Range", byteRange)
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
	if readResp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("Accept-Ranges=%q", readResp.Header.Get("Accept-Ranges"))
	}
	if readResp.ContentLength != int64(len(payload)) {
		t.Fatalf("Content-Length=%d want %d", readResp.ContentLength, len(payload))
	}
	got, _ := io.ReadAll(readResp.Body)
	if !bytes.Equal(got, payload) {
		t.Fatalf("download = %q", got)
	}
	readCommand := map[string]any{
		"command": "fileRead", "target": "Movies", "parameter": testFileDocID,
		"detail": map[string]any{"filename": "photo.png"},
	}
	for _, tc := range []struct {
		name, header, contentRange string
		want                       []byte
	}{
		{"closed", "bytes=1-3", "bytes 1-3/" + strconv.Itoa(len(payload)), payload[1:4]},
		{"open-ended", "bytes=4-", "bytes 4-13/" + strconv.Itoa(len(payload)), payload[4:]},
		{"suffix", "bytes=-4", "bytes " + strconv.Itoa(len(payload)-4) + "-" + strconv.Itoa(len(payload)-1) + "/" + strconv.Itoa(len(payload)), payload[len(payload)-4:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rangeResp := postJSONCommandWithRange(t, ts.URL, tok, readCommand, tc.header)
			defer rangeResp.Body.Close()
			rangeBody, err := io.ReadAll(rangeResp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if rangeResp.StatusCode != http.StatusPartialContent || !bytes.Equal(rangeBody, tc.want) {
				t.Fatalf("range status=%d body=%q want=%q", rangeResp.StatusCode, rangeBody, tc.want)
			}
			if got := rangeResp.Header.Get("Content-Range"); got != tc.contentRange {
				t.Fatalf("Content-Range=%q want=%q", got, tc.contentRange)
			}
			if rangeResp.ContentLength != int64(len(tc.want)) {
				t.Fatalf("Content-Length=%d want=%d", rangeResp.ContentLength, len(tc.want))
			}
			if rangeResp.Header.Get("Accept-Ranges") != "bytes" ||
				rangeResp.Header.Get("X-DatoriumDB-SHA256") == "" ||
				rangeResp.Header.Get("X-DatoriumDB-File-Version") != version ||
				rangeResp.Header.Get("ETag") != `"`+version+`"` {
				t.Fatalf("partial response missing file metadata headers: %v", rangeResp.Header)
			}
		})
	}

	for _, rangeHeader := range []string{"bytes=999-", "bytes=0-1,4-5"} {
		t.Run("reject "+rangeHeader, func(t *testing.T) {
			invalidResp := postJSONCommandWithRange(t, ts.URL, tok, readCommand, rangeHeader)
			if invalidResp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
				t.Fatalf("invalid range status=%d", invalidResp.StatusCode)
			}
			if got := invalidResp.Header.Get("Content-Range"); got != "bytes */"+strconv.Itoa(len(payload)) {
				t.Fatalf("invalid Content-Range=%q", got)
			}
			invalidEnv := decodeEnvelope(t, invalidResp)
			if invalidEnv["ok"] != false || firstErrCode(t, invalidEnv) != "invalidRange" {
				t.Fatalf("invalid range envelope=%#v", invalidEnv)
			}
		})
	}

	t.Run("ignore unknown range unit", func(t *testing.T) {
		resp := postJSONCommandWithRange(t, ts.URL, tok, readCommand, "items=0-1")
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, payload) {
			t.Fatalf("unknown unit status=%d body=%q", resp.StatusCode, body)
		}
		if resp.Header.Get("Content-Range") != "" {
			t.Fatalf("full body must not set Content-Range, got %q", resp.Header.Get("Content-Range"))
		}
	})

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
