package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// backend returns an httptest server that echoes a fixed tag, and its host:port.
func backend(t *testing.T, tag string) (string, func()) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, tag)
	}))
	return s.Listener.Addr().String(), s.Close
}

func do(t *testing.T, a *awsRouter, rt awsRoute, method, path string, headers map[string]string, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.serve(rec, req, rt)
	return rec.Code, rec.Body.String()
}

func TestIngressDynamoDBRoutesByTableName(t *testing.T) {
	a := newAWSRouter(t.TempDir())
	users, cu := backend(t, "USERS")
	defer cu()
	orders, co := backend(t, "ORDERS")
	defer co()
	rt := awsRoute{Engine: "dynamodb", Resources: map[string]string{"users": users, "orders": orders}}

	jsonHdr := map[string]string{"X-Amz-Target": "DynamoDB_20120810.GetItem", "Content-Type": "application/x-amz-json-1.0"}
	if _, b := do(t, a, rt, "POST", "/", jsonHdr, `{"TableName":"orders"}`); b != "ORDERS" {
		t.Fatalf("GetItem on orders routed to %q", b)
	}
	if _, b := do(t, a, rt, "POST", "/", jsonHdr, `{"TableName":"users"}`); b != "USERS" {
		t.Fatalf("GetItem on users routed to %q", b)
	}
	// ListTables is synthesized from the route table (both backends' tables).
	lt := map[string]string{"X-Amz-Target": "DynamoDB_20120810.ListTables", "Content-Type": "application/x-amz-json-1.0"}
	_, body := do(t, a, rt, "POST", "/", lt, `{}`)
	if !strings.Contains(body, "users") || !strings.Contains(body, "orders") {
		t.Fatalf("ListTables didn't synthesize both tables: %s", body)
	}
}

func TestIngressLambdaRoutesByPath(t *testing.T) {
	a := newAWSRouter(t.TempDir())
	fn, cf := backend(t, "FN")
	defer cf()
	rt := awsRoute{Engine: "lambda", Resources: map[string]string{"myfn": fn}}

	if _, b := do(t, a, rt, "POST", "/2015-03-31/functions/myfn/invocations", nil, `{}`); b != "FN" {
		t.Fatalf("invoke routed to %q", b)
	}
	// ListFunctions (GET the collection) is synthesized.
	_, body := do(t, a, rt, "GET", "/2015-03-31/functions", nil, "")
	if !strings.Contains(body, "myfn") {
		t.Fatalf("ListFunctions didn't include myfn: %s", body)
	}
}

func TestIngressSecretsRoutesByIDIncludingARN(t *testing.T) {
	a := newAWSRouter(t.TempDir())
	sec, cs := backend(t, "SECRET")
	defer cs()
	rt := awsRoute{Engine: "secretsmanager", Resources: map[string]string{"db-secret": sec}}

	jsonHdr := map[string]string{"X-Amz-Target": "secretsmanager.GetSecretValue", "Content-Type": "application/x-amz-json-1.1"}
	if _, b := do(t, a, rt, "POST", "/", jsonHdr, `{"SecretId":"db-secret"}`); b != "SECRET" {
		t.Fatalf("by name routed to %q", b)
	}
	// A full ARN (with random suffix) must resolve to the same secret name.
	arn := `{"SecretId":"arn:aws:secretsmanager:us-east-1:000000000000:secret:db-secret-AbCdEf"}`
	if _, b := do(t, a, rt, "POST", "/", jsonHdr, arn); b != "SECRET" {
		t.Fatalf("by ARN routed to %q", b)
	}
}

func TestIngressSingleTargetForKMS(t *testing.T) {
	a := newAWSRouter(t.TempDir())
	kms, ck := backend(t, "KMS")
	defer ck()
	rt := awsRoute{Engine: "kms", Resources: map[string]string{"mykey": kms}}

	// A KMS Decrypt carries no KeyId — it must still reach the single backend.
	jsonHdr := map[string]string{"X-Amz-Target": "TrentService.Decrypt", "Content-Type": "application/x-amz-json-1.1"}
	if _, b := do(t, a, rt, "POST", "/", jsonHdr, `{"CiphertextBlob":"..."}`); b != "KMS" {
		t.Fatalf("Decrypt (no KeyId) routed to %q, want the single backend", b)
	}
}
