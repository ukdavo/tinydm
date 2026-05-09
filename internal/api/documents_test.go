package api_test

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

// scaffold creates a tenant/project/bucket and returns their IDs along with
// an admin bearer token — ready for document operations.
func scaffold(t *testing.T, ts *testServer) (tenantID, projID, bucketID, token string) {
	t.Helper()

	tenant, _ := ts.seedAdminUser(t, "DocTenant", "admin", "pass")
	token = ts.login(t, tenant.ID, "admin", "pass")
	tenantID = tenant.ID

	var proj map[string]any
	ts.doJSON(t, http.MethodPost,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenantID),
		map[string]string{"name": "MyProject"}, bearer(token), &proj)
	projID = proj["id"].(string)

	var bucket map[string]any
	ts.doJSON(t, http.MethodPost,
		fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets", tenantID, projID),
		map[string]string{"name": "MyBucket"}, bearer(token), &bucket)
	bucketID = bucket["id"].(string)

	return tenantID, projID, bucketID, token
}

// docsPath returns the base documents path for a bucket.
func docsPath(tenantID, projID, bucketID string) string {
	return fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets/%s/documents",
		tenantID, projID, bucketID)
}

// docPath returns the path for a specific document.
func docPath(tenantID, projID, bucketID, docID string) string {
	return docsPath(tenantID, projID, bucketID) + "/" + docID
}

// ─── Upload / Download / Delete ───────────────────────────────────────────────

func TestDocuments_UploadAndList(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	content := []byte("hello, world")
	doc := ts.uploadFile(t, token, tid, pid, bid, "hello.txt", content)

	if doc["id"] == nil {
		t.Error("expected id in uploaded document")
	}
	if doc["name"] != "hello.txt" {
		t.Errorf("name: got %v, want hello.txt", doc["name"])
	}
	if doc["size"] == nil {
		t.Error("expected size in uploaded document")
	}

	// List documents — should include the uploaded file.
	var list map[string]any
	resp := ts.doJSON(t, http.MethodGet, docsPath(tid, pid, bid), nil, bearer(token), &list)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	data := list["data"].([]any)
	if len(data) != 1 {
		t.Errorf("expected 1 document in list, got %d", len(data))
	}
}

func TestDocuments_Download(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	original := []byte("download me")
	doc := ts.uploadFile(t, token, tid, pid, bid, "get.txt", original)
	docID := doc["id"].(string)

	resp := ts.do(t, http.MethodGet,
		docPath(tid, pid, bid, docID)+"/content",
		nil, bearer(token))
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("content: got %q, want %q", got, original)
	}
}

func TestDocuments_Get(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	doc := ts.uploadFile(t, token, tid, pid, bid, "meta.txt", []byte("content"))
	docID := doc["id"].(string)

	var fetched map[string]any
	resp := ts.doJSON(t, http.MethodGet,
		docPath(tid, pid, bid, docID),
		nil, bearer(token), &fetched)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	if fetched["id"] != docID {
		t.Errorf("id: got %v, want %s", fetched["id"], docID)
	}
	if fetched["name"] != "meta.txt" {
		t.Errorf("name: got %v, want meta.txt", fetched["name"])
	}
}

func TestDocuments_Delete(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	doc := ts.uploadFile(t, token, tid, pid, bid, "bye.txt", []byte("goodbye"))
	docID := doc["id"].(string)

	resp := ts.do(t, http.MethodDelete,
		docPath(tid, pid, bid, docID),
		nil, bearer(token))
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNoContent)

	// Should now be gone.
	resp2 := ts.do(t, http.MethodGet,
		docPath(tid, pid, bid, docID),
		nil, bearer(token))
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusNotFound)
}

func TestDocuments_Download_NotFound(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	resp := ts.do(t, http.MethodGet,
		docsPath(tid, pid, bid)+"/does-not-exist/content",
		nil, bearer(token))
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNotFound)
}

// ─── Search ───────────────────────────────────────────────────────────────────

func TestDocuments_Search(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	ts.uploadFile(t, token, tid, pid, bid, "report-2024.txt", []byte("a"))
	ts.uploadFile(t, token, tid, pid, bid, "invoice-2024.txt", []byte("b"))
	ts.uploadFile(t, token, tid, pid, bid, "readme.md", []byte("c"))

	var result map[string]any
	resp := ts.doJSON(t, http.MethodGet,
		docsPath(tid, pid, bid)+"?q=2024",
		nil, bearer(token), &result)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	data := result["data"].([]any)
	if len(data) != 2 {
		t.Errorf("search ?q=2024: got %d results, want 2", len(data))
	}
}

// ─── Tags ─────────────────────────────────────────────────────────────────────

func TestDocuments_Tags(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	doc := ts.uploadFile(t, token, tid, pid, bid, "tagged.txt", []byte("content"))
	docID := doc["id"].(string)
	tagBase := docPath(tid, pid, bid, docID) + "/tags"

	// Add a tag.
	resp := ts.do(t, http.MethodPost, tagBase+"/important", nil, bearer(token))
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	// Add a second tag.
	resp2 := ts.do(t, http.MethodPost, tagBase+"/draft", nil, bearer(token))
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusOK)

	// List tags.
	var tags []any
	resp3 := ts.doJSON(t, http.MethodGet, tagBase, nil, bearer(token), &tags)
	defer resp3.Body.Close()
	assertStatus(t, resp3, http.StatusOK)
	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}

	// Remove one tag.
	resp4 := ts.do(t, http.MethodDelete, tagBase+"/draft", nil, bearer(token))
	defer resp4.Body.Close()
	assertStatus(t, resp4, http.StatusNoContent)

	// Confirm removal.
	var tagsAfter []any
	resp5 := ts.doJSON(t, http.MethodGet, tagBase, nil, bearer(token), &tagsAfter)
	defer resp5.Body.Close()
	if len(tagsAfter) != 1 {
		t.Errorf("expected 1 tag after removal, got %d", len(tagsAfter))
	}
}

func TestDocuments_TagFilter(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	d1 := ts.uploadFile(t, token, tid, pid, bid, "a.txt", []byte("a"))
	d2 := ts.uploadFile(t, token, tid, pid, bid, "b.txt", []byte("b"))

	// Tag only d1.
	ts.do(t, http.MethodPost,
		docPath(tid, pid, bid, d1["id"].(string))+"/tags/featured",
		nil, bearer(token))

	// d2 gets a different tag.
	ts.do(t, http.MethodPost,
		docPath(tid, pid, bid, d2["id"].(string))+"/tags/archived",
		nil, bearer(token))

	var result map[string]any
	resp := ts.doJSON(t, http.MethodGet,
		docsPath(tid, pid, bid)+"?tag=featured",
		nil, bearer(token), &result)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	data := result["data"].([]any)
	if len(data) != 1 {
		t.Errorf("tag filter: got %d results, want 1", len(data))
	}
}

// ─── Custom properties ────────────────────────────────────────────────────────

func TestDocuments_Properties(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	doc := ts.uploadFile(t, token, tid, pid, bid, "props.txt", []byte("content"))
	docID := doc["id"].(string)
	propBase := docPath(tid, pid, bid, docID) + "/properties"

	// Set a property.
	resp := ts.doJSON(t, http.MethodPut, propBase+"/author",
		map[string]string{"value": "Alice"},
		bearer(token), nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	// Get all properties.
	var props map[string]any
	resp2 := ts.doJSON(t, http.MethodGet, propBase, nil, bearer(token), &props)
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusOK)
	if props["author"] != "Alice" {
		t.Errorf("author: got %v, want Alice", props["author"])
	}

	// Delete the property.
	resp3 := ts.do(t, http.MethodDelete, propBase+"/author", nil, bearer(token))
	defer resp3.Body.Close()
	assertStatus(t, resp3, http.StatusNoContent)

	// Confirm deletion.
	var propsAfter map[string]any
	resp4 := ts.doJSON(t, http.MethodGet, propBase, nil, bearer(token), &propsAfter)
	defer resp4.Body.Close()
	if _, ok := propsAfter["author"]; ok {
		t.Error("expected author property to be deleted")
	}
}

// ─── Version history & restore ────────────────────────────────────────────────

func TestDocuments_Versions(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	// Upload initial version.
	doc := ts.uploadFile(t, token, tid, pid, bid, "versioned.txt", []byte("version 1"))
	docID := doc["id"].(string)

	// Update (replace content) — creates a snapshot.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "versioned.txt")
	fw.Write([]byte("version 2"))
	mw.Close()

	updateResp := ts.do(t, http.MethodPut,
		docPath(tid, pid, bid, docID),
		&buf, map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  mw.FormDataContentType(),
		})
	defer updateResp.Body.Close()
	assertStatus(t, updateResp, http.StatusOK)

	// List versions — should have one snapshot (v1).
	var versions map[string]any
	resp := ts.doJSON(t, http.MethodGet,
		docPath(tid, pid, bid, docID)+"/versions",
		nil, bearer(token), &versions)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	versionData := versions["data"].([]any)
	if len(versionData) == 0 {
		t.Fatal("expected at least one version snapshot")
	}

	// Confirm current content is v2.
	downloadResp := ts.do(t, http.MethodGet,
		docPath(tid, pid, bid, docID)+"/content",
		nil, bearer(token))
	defer downloadResp.Body.Close()
	current, _ := io.ReadAll(downloadResp.Body)
	if string(current) != "version 2" {
		t.Errorf("current content: got %q, want %q", current, "version 2")
	}
}

func TestDocuments_RestoreVersion(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	// v1 upload.
	doc := ts.uploadFile(t, token, tid, pid, bid, "restore.txt", []byte("original content"))
	docID := doc["id"].(string)

	// v2 update.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "restore.txt")
	fw.Write([]byte("updated content"))
	mw.Close()

	ts.do(t, http.MethodPut,
		docPath(tid, pid, bid, docID),
		&buf, map[string]string{
			"Authorization": "Bearer " + token,
			"Content-Type":  mw.FormDataContentType(),
		})

	// Get version list.
	var versions map[string]any
	ts.doJSON(t, http.MethodGet,
		docPath(tid, pid, bid, docID)+"/versions",
		nil, bearer(token), &versions)

	versionData := versions["data"].([]any)
	if len(versionData) == 0 {
		t.Fatal("no versions to restore")
	}
	v1 := versionData[0].(map[string]any)
	versionID := v1["id"].(string)

	// Restore v1.
	restoreResp := ts.do(t, http.MethodPost,
		docPath(tid, pid, bid, docID)+"/versions/"+versionID+"/restore",
		nil, bearer(token))
	defer restoreResp.Body.Close()
	assertStatus(t, restoreResp, http.StatusOK)

	// Verify restored content matches v1.
	dlResp := ts.do(t, http.MethodGet,
		docPath(tid, pid, bid, docID)+"/content",
		nil, bearer(token))
	defer dlResp.Body.Close()
	restored, _ := io.ReadAll(dlResp.Body)
	if string(restored) != "original content" {
		t.Errorf("restored content: got %q, want %q", restored, "original content")
	}
}

// ─── Pagination ───────────────────────────────────────────────────────────────

func TestDocuments_Pagination(t *testing.T) {
	ts := newTestServer(t)
	tid, pid, bid, token := scaffold(t, ts)

	// Upload 5 documents.
	for i := 0; i < 5; i++ {
		ts.uploadFile(t, token, tid, pid, bid,
			fmt.Sprintf("doc-%02d.txt", i), []byte("x"))
	}

	// Request only 2.
	var page1 map[string]any
	resp := ts.doJSON(t, http.MethodGet,
		docsPath(tid, pid, bid)+"?limit=2&offset=0",
		nil, bearer(token), &page1)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	data := page1["data"].([]any)
	if len(data) != 2 {
		t.Errorf("page 1: got %d docs, want 2", len(data))
	}

	pag := page1["pagination"].(map[string]any)
	if pag["total"].(float64) != 5 {
		t.Errorf("total: got %v, want 5", pag["total"])
	}
	if pag["has_more"].(bool) != true {
		t.Error("has_more: expected true for page 1 of 5")
	}

	// Last page.
	var page3 map[string]any
	resp2 := ts.doJSON(t, http.MethodGet,
		docsPath(tid, pid, bid)+"?limit=2&offset=4",
		nil, bearer(token), &page3)
	defer resp2.Body.Close()

	data3 := page3["data"].([]any)
	if len(data3) != 1 {
		t.Errorf("last page: got %d docs, want 1", len(data3))
	}
	pag3 := page3["pagination"].(map[string]any)
	if pag3["has_more"].(bool) != false {
		t.Error("has_more: expected false on last page")
	}
}
