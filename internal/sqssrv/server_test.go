package sqssrv

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := bolt.Open(filepath.Join(t.TempDir(), "sqs.bolt"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ts := httptest.NewServer(&server{store: newStore(db)})
	t.Cleanup(ts.Close)
	return ts
}

// query POSTs a Query-protocol (form) request and returns the XML body.
func query(t *testing.T, base string, form url.Values) string {
	t.Helper()
	resp, err := http.PostForm(base+"/", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("query %s -> %s\n%s", form.Get("Action"), resp.Status, b)
	}
	return string(b)
}

// jsonCall POSTs an AWS JSON 1.0 request and returns the JSON body.
func jsonCall(t *testing.T, base, action, body string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS."+action)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("json %s -> %s\n%s", action, resp.Status, b)
	}
	return string(b)
}

func TestQueryProtocol(t *testing.T) {
	ts := testServer(t)

	// CreateQueue (XML response carries the queue URL).
	out := query(t, ts.URL, url.Values{"Action": {"CreateQueue"}, "QueueName": {"jobs"}})
	if !strings.Contains(out, "<QueueUrl>") || !strings.Contains(out, "/jobs") {
		t.Fatalf("CreateQueue XML missing QueueUrl:\n%s", out)
	}
	qurl := ts.URL + "/000000000000/jobs"

	// SendMessage.
	out = query(t, ts.URL, url.Values{"Action": {"SendMessage"}, "QueueUrl": {qurl}, "MessageBody": {"hi"}})
	if !strings.Contains(out, "<MD5OfMessageBody>49f68a5c8493ec2c0bf489821c21fc3b</MD5OfMessageBody>") {
		t.Fatalf("SendMessage MD5 wrong:\n%s", out)
	}

	// ReceiveMessage.
	out = query(t, ts.URL, url.Values{"Action": {"ReceiveMessage"}, "QueueUrl": {qurl}})
	if !strings.Contains(out, "<Body>hi</Body>") || !strings.Contains(out, "<ReceiptHandle>") {
		t.Fatalf("ReceiveMessage XML wrong:\n%s", out)
	}
}

func TestJSONProtocol(t *testing.T) {
	ts := testServer(t)
	qurl := ts.URL + "/000000000000/jobs"

	jsonCall(t, ts.URL, "CreateQueue", `{"QueueName":"jobs"}`)
	send := jsonCall(t, ts.URL, "SendMessage", `{"QueueUrl":"`+qurl+`","MessageBody":"hi"}`)
	if !strings.Contains(send, `"MD5OfMessageBody":"49f68a5c8493ec2c0bf489821c21fc3b"`) {
		t.Fatalf("JSON SendMessage MD5 wrong:\n%s", send)
	}
	recv := jsonCall(t, ts.URL, "ReceiveMessage", `{"QueueUrl":"`+qurl+`","MaxNumberOfMessages":1}`)
	if !strings.Contains(recv, `"Body":"hi"`) || !strings.Contains(recv, `"ReceiptHandle"`) {
		t.Fatalf("JSON ReceiveMessage wrong:\n%s", recv)
	}
}
