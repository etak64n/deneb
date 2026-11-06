package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/etak64n/deneb/internal/app"
	"github.com/etak64n/deneb/internal/store"
)

// One upload of a video at a time: a second PUT while the first sends its file gets
// 409, and the file of the first stays.
func TestPutUploadsOneAtATime(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	var created createdVideo
	h.dashboard("POST", "/v1/videos", map[string]string{"title": "Clip"}).json(t, &created)
	part := bytes.Repeat([]byte("frame "), 1000)

	body, writer := io.Pipe()
	first := make(chan int, 1)
	go func() {
		request, _ := http.NewRequest("PUT", created.Upload.URL, body)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			first <- 0
			return
		}
		response.Body.Close()
		first <- response.StatusCode
	}()
	writer.Write(part)
	// The first PUT takes the lock before it reads its body.
	time.Sleep(300 * time.Millisecond)
	if r := h.do("PUT", strings.TrimPrefix(created.Upload.URL, h.url), part); r.status != http.StatusConflict {
		t.Errorf("a second PUT at the same time: %d %s", r.status, r.body)
	}
	writer.Write(part)
	writer.Close()
	if status := <-first; status != http.StatusOK {
		t.Fatalf("the first PUT: %d", status)
	}
	if stored, err := os.ReadFile(h.app.UploadPath(created.Video.ID)); err != nil || len(stored) != 2*len(part) {
		t.Errorf("the upload: %d bytes, %v", len(stored), err)
	}

	// The finished upload is the file that the encoder reads, which tus may not remove.
	if r := h.do("DELETE", strings.TrimPrefix(created.Upload.URL, h.url), nil, "Tus-Resumable", "1.0.0"); r.status != http.StatusConflict {
		t.Errorf("DELETE of a finished upload: %d %s", r.status, r.body)
	}
	if _, err := os.Stat(h.app.UploadPath(created.Video.ID)); err != nil {
		t.Errorf("the upload after a DELETE: %v", err)
	}
}

func TestRetryVideo(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	var created createdVideo
	h.dashboard("POST", "/v1/videos", map[string]string{"title": "Clip"}).json(t, &created)
	id := created.Video.ID
	if r := h.dashboard("POST", "/v1/videos/"+id+"/retry", nil); r.status != http.StatusConflict {
		t.Errorf("retry of a video that waits for its file: %d %s", r.status, r.body)
	}
	if r := h.do("PUT", strings.TrimPrefix(created.Upload.URL, h.url), []byte("frames")); r.status != http.StatusOK {
		t.Fatalf("PUT: %d %s", r.status, r.body)
	}
	h.app.Store().FailVideo(context.Background(), id, "the bucket did not answer", time.Now())
	var retried struct {
		Video app.VideoView `json:"video"`
	}
	r := h.dashboard("POST", "/v1/videos/"+id+"/retry", nil)
	r.json(t, &retried)
	if r.status != http.StatusOK || retried.Video.Status != store.VideoQueued || retried.Video.Error != "" {
		t.Errorf("retry of a failed video: %d %+v", r.status, retried.Video)
	}
	if r := h.dashboard("POST", "/v1/videos/vid_missing/retry", nil); r.status != http.StatusNotFound {
		t.Errorf("retry of a missing video: %d", r.status)
	}
}

// A webhook may go to a private network, where its receiver often runs, but not to
// the metadata service of a cloud.
func TestWebhookAddresses(t *testing.T) {
	h := newHarness(t, nil)
	h.signIn()
	r := h.dashboard("POST", "/v1/webhooks", map[string]string{"url": "http://169.254.169.254/latest/meta-data/"})
	if r.status != http.StatusBadRequest || !strings.Contains(string(r.body), "link-local") {
		t.Errorf("the metadata service: %d %s", r.status, r.body)
	}
	if r := h.dashboard("POST", "/v1/webhooks", map[string]string{"url": "http://10.0.0.5/hooks/deneb"}); r.status != http.StatusCreated {
		t.Errorf("a private address: %d %s", r.status, r.body)
	}
}
