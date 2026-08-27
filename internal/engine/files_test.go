package engine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JohnAD/datoriumdb/internal/commandreq"
	"github.com/JohnAD/datoriumdb/internal/replication"
)

func TestPutFileListReadDeleteLifecycle(t *testing.T) {
	eng := testEngine(t)
	id := "01TESTMOVIES000000000000F1"
	created := eng.Execute(commandreq.Must("create", "Movies", id, map[string]any{
		"$": "Movies:0", "title": "With File", "releaseYear": 2001, "status": "released",
	}))
	if created["ok"] != true {
		t.Fatalf("create: %#v", created)
	}

	put := eng.PutFile(bytes.NewReader([]byte("payload")), "Movies", id, "note.txt", PutFileOptions{
		ContentType: "text/plain",
	})
	if put["ok"] != true {
		t.Fatalf("PutFile: %#v", put)
	}
	ver, _ := put["version"].(string)

	listed := eng.ListFiles("Movies", id)
	if listed["ok"] != true {
		t.Fatalf("ListFiles: %#v", listed)
	}

	entry, f, errRes := eng.OpenFileForDownload("Movies", id, "note.txt")
	if errRes != nil {
		t.Fatalf("OpenFileForDownload: %#v", *errRes)
	}
	defer f.Close()
	if entry.Name != "note.txt" || entry.Version != ver {
		t.Fatalf("unexpected entry %#v", entry)
	}

	del := eng.DeleteFile("Movies", id, "note.txt", ver, "")
	if del["ok"] != true {
		t.Fatalf("DeleteFile: %#v", del)
	}
}

func TestFileCommandsWrongMachine(t *testing.T) {
	eng, low, high := multiServerEngine(t, "serverC")
	id := idInSlotRange(t, low, high) // low range SOT is serverA, not serverC

	put := eng.PutFile(strings.NewReader("x"), "Movies", id, "a.txt", PutFileOptions{ContentType: "text/plain"})
	if put["ok"] != false || firstErrorCode(put) != "wrongMachine" {
		t.Fatalf("expected PutFile wrongMachine: %#v", put)
	}
	if put["filename"] != "a.txt" || put["command"] != "fileCreate" {
		t.Fatalf("expected filename/command on bounce: %#v", put)
	}

	listed := eng.ListFiles("Movies", id)
	if listed["ok"] != false || firstErrorCode(listed) != "wrongMachine" {
		t.Fatalf("expected ListFiles wrongMachine: %#v", listed)
	}

	_, _, errRes := eng.OpenFileForDownload("Movies", id, "a.txt")
	if errRes == nil || firstErrorCode(*errRes) != "wrongMachine" {
		t.Fatalf("expected OpenFileForDownload wrongMachine: %#v", errRes)
	}
	if (*errRes)["filename"] != "a.txt" {
		t.Fatalf("expected filename on fileRead bounce: %#v", *errRes)
	}

	del := eng.DeleteFile("Movies", id, "a.txt", "v1", "")
	if del["ok"] != false || firstErrorCode(del) != "wrongMachine" {
		t.Fatalf("expected DeleteFile wrongMachine: %#v", del)
	}

	_ = high
}

func TestOpenFileForDownloadStale(t *testing.T) {
	eng := testEngine(t)
	id := "01TESTMOVIES000000000000F2"
	created := eng.Execute(commandreq.Must("create", "Movies", id, map[string]any{
		"$": "Movies:0", "title": "Stale", "releaseYear": 2001, "status": "released",
	}))
	if created["ok"] != true {
		t.Fatalf("create: %#v", created)
	}
	put := eng.PutFile(bytes.NewReader([]byte("x")), "Movies", id, "a.txt", PutFileOptions{ContentType: "text/plain"})
	if put["ok"] != true {
		t.Fatalf("PutFile: %#v", put)
	}

	state := &replication.ReadMemberState{}
	state.MarkPendingFile("Movies", id, "a.txt")
	eng.ReadState = state

	_, _, errRes := eng.OpenFileForDownload("Movies", id, "a.txt")
	if errRes == nil || firstErrorCode(*errRes) != "fileStale" {
		t.Fatalf("expected fileStale: %#v", errRes)
	}

	listed := eng.ListFiles("Movies", id)
	if listed["ok"] != false || firstErrorCode(listed) != "fileStale" {
		t.Fatalf("expected ListFiles fileStale: %#v", listed)
	}
}

func TestOpenFileForDownloadValidation(t *testing.T) {
	eng := testEngine(t)
	_, _, errRes := eng.OpenFileForDownload("Movies", "../evil", "a.txt")
	if errRes == nil || firstErrorCode(*errRes) != "invalidDocumentId" {
		t.Fatalf("expected invalidDocumentId: %#v", errRes)
	}
	_, _, errRes = eng.OpenFileForDownload("Movies", "01TESTMOVIES000000000000F3", "../evil")
	if errRes == nil || firstErrorCode(*errRes) != "invalidFileName" {
		t.Fatalf("expected invalidFileName: %#v", errRes)
	}
	_, _, errRes = eng.OpenFileForDownload("Movies", "01TESTMOVIES000000000000F3", "missing.txt")
	if errRes == nil || firstErrorCode(*errRes) != "documentNotFound" {
		t.Fatalf("expected documentNotFound: %#v", errRes)
	}
}
