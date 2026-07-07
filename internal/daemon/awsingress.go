// The AWS single-endpoint router: gives each built-in AWS service one endpoint
// per stack — s3.<stack>.doze, sqs.<stack>.doze,
// sns.<stack>.doze — served on the shared :80 ingress and routed to the
// right per-resource backend, exactly like real AWS puts every bucket under
// s3.amazonaws.com. So a stock SDK/CLI works with a single, port-less
// AWS_ENDPOINT_URL_S3=http://s3.<stack>.doze.
//
// doze runs one backend per bucket/queue/topic, so the router extracts the
// resource from each request (S3: first path segment; SQS: QueueName/QueueUrl
// in the body; SNS: TopicArn/Name in the body) and forwards to that backend —
// which is host-aware, so the URLs it returns already carry the shared host.
// Resource-list operations are synthesized from the known resource set.
package daemon

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
)

// awsTypes are the built-in AWS engines that get a shared endpoint.
var awsTypes = map[string]bool{"s3": true, "sqs": true, "sns": true}

// awsRoute is one type-host's resource→backend table (e.g. s3.demo.doze:
// {uploads: 127.0.0.5:9000, thumbs: 127.0.0.6:9000}).
type awsRoute struct {
	Engine    string            `json:"engine"`
	Resources map[string]string `json:"resources"` // resource name -> backend "ip:port"
	PID       int               `json:"pid"`
}

func awsRoutesPath(home string) string { return filepath.Join(home, "aws-ingress.json") }

// publishAWSRoutes records this stack's type-host tables, dropping this pid's
// prior entries and any dead daemon's.
func publishAWSRoutes(home string, routes map[string]awsRoute, pid int) {
	all := readAWSRoutes(home)
	for host, r := range all {
		if r.PID == pid || !pidAlive(r.PID) {
			delete(all, host)
		}
	}
	for host, r := range routes {
		all[host] = r
	}
	writeAWSRoutes(home, all)
}

func unpublishAWSRoutes(home string, pid int) {
	all := readAWSRoutes(home)
	for host, r := range all {
		if r.PID == pid {
			delete(all, host)
		}
	}
	writeAWSRoutes(home, all)
}

func readAWSRoutes(home string) map[string]awsRoute {
	out := map[string]awsRoute{}
	if data, err := os.ReadFile(awsRoutesPath(home)); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func writeAWSRoutes(home string, all map[string]awsRoute) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return
	}
	if data, err := json.MarshalIndent(all, "", "  "); err == nil {
		tmp := awsRoutesPath(home) + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, awsRoutesPath(home))
		}
	}
}

// awsRouter serves the shared AWS endpoints, reloading the shared table lazily.
type awsRouter struct {
	home    string
	mu      sync.Mutex
	loaded  time.Time
	routes  map[string]awsRoute
	proxies map[string]*httputil.ReverseProxy
}

func newAWSRouter(home string) *awsRouter {
	return &awsRouter{home: home, proxies: map[string]*httputil.ReverseProxy{}}
}

// route returns the type-host table for host, if this is an AWS endpoint.
func (a *awsRouter) route(host string) (awsRoute, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Since(a.loaded) > time.Second {
		a.routes = readAWSRoutes(a.home)
		a.loaded = time.Now()
	}
	r, ok := a.routes[host]
	return r, ok
}

func (a *awsRouter) proxyTo(target string) *httputil.ReverseProxy {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.proxies[target]
	if p == nil {
		p = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: target})
		a.proxies[target] = p
	}
	return p
}

// serve routes one request for the given type-host to the resource's backend.
func (a *awsRouter) serve(w http.ResponseWriter, r *http.Request, rt awsRoute) {
	switch rt.Engine {
	case "s3":
		a.serveS3(w, r, rt)
	case "sqs":
		a.serveSQS(w, r, rt)
	case "sns":
		a.serveSNS(w, r, rt)
	default:
		http.Error(w, "doze: unknown AWS engine "+rt.Engine, http.StatusNotFound)
	}
}

// --- S3: bucket is the first path segment (path-style addressing) ---

func (a *awsRouter) serveS3(w http.ResponseWriter, r *http.Request, rt awsRoute) {
	seg := firstSegment(r.URL.Path)
	if seg == "" {
		// GET / → ListBuckets: synthesize from the known bucket set.
		a.listBuckets(w, rt)
		return
	}
	target, ok := rt.Resources[seg]
	if !ok {
		s3Error(w, "NoSuchBucket", "The specified bucket does not exist", http.StatusNotFound, "/"+seg)
		return
	}
	a.proxyTo(target).ServeHTTP(w, r)
}

type s3ListBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Owner   struct {
		ID          string `xml:"ID"`
		DisplayName string `xml:"DisplayName"`
	} `xml:"Owner"`
	Buckets struct {
		Bucket []s3Bucket `xml:"Bucket"`
	} `xml:"Buckets"`
}

type s3Bucket struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

func (a *awsRouter) listBuckets(w http.ResponseWriter, rt awsRoute) {
	var res s3ListBucketsResult
	res.Owner.ID = "doze"
	res.Owner.DisplayName = "doze"
	for name := range rt.Resources {
		res.Buckets.Bucket = append(res.Buckets.Bucket, s3Bucket{
			Name:         name,
			CreationDate: "2020-01-01T00:00:00.000Z",
		})
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(res)
}

func s3Error(w http.ResponseWriter, code, msg string, status int, resource string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, xml.Header)
	_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>%s</Message><Resource>%s</Resource></Error>", code, msg, resource)
}

func firstSegment(path string) string {
	p := strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

// awsAccountID matches the built-in AWS services' fixed account (awslocal).
const awsAccountID = "000000000000"

// readBody reads and restores r.Body so it can still be forwarded.
func readBody(r *http.Request) []byte {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

// awsParam pulls a field from the request by name, across both the JSON
// protocol (X-Amz-Target + JSON body, modern SDKs) and the legacy query
// protocol (form-encoded body). Returns "" if absent.
func awsParam(r *http.Request, body []byte, key string) string {
	if r.Header.Get("X-Amz-Target") != "" || strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-amz-json") {
		var obj map[string]any
		if json.Unmarshal(body, &obj) == nil {
			if v, ok := obj[key].(string); ok {
				return v
			}
		}
		return ""
	}
	if vals, err := url.ParseQuery(string(body)); err == nil {
		if v := vals.Get(key); v != "" {
			return v
		}
	}
	return r.URL.Query().Get(key)
}

// awsAction returns the operation name from either protocol.
func awsAction(r *http.Request, body []byte) string {
	if t := r.Header.Get("X-Amz-Target"); t != "" {
		if i := strings.LastIndexByte(t, '.'); i >= 0 {
			return t[i+1:]
		}
		return t
	}
	return awsParam(r, body, "Action")
}

// lastPathSegment returns the final segment of a URL path (the resource name in
// an SQS QueueUrl).
func lastPathSegment(s string) string {
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// --- SQS: queue is QueueUrl's last path segment, or QueueName ---

func (a *awsRouter) serveSQS(w http.ResponseWriter, r *http.Request, rt awsRoute) {
	body := readBody(r)
	if awsAction(r, body) == "ListQueues" {
		a.listQueues(w, r, rt)
		return
	}
	queue := ""
	if u := awsParam(r, body, "QueueUrl"); u != "" {
		queue = lastPathSegment(u)
	} else {
		queue = awsParam(r, body, "QueueName")
	}
	target, ok := rt.Resources[queue]
	if !ok {
		awsError(w, r, "AWS.SimpleQueueService.NonExistentQueue", "The specified queue does not exist.", http.StatusBadRequest)
		return
	}
	a.proxyTo(target).ServeHTTP(w, r)
}

func (a *awsRouter) listQueues(w http.ResponseWriter, r *http.Request, rt awsRoute) {
	urls := make([]string, 0, len(rt.Resources))
	for name := range rt.Resources {
		urls = append(urls, "http://"+r.Host+"/"+awsAccountID+"/"+name)
	}
	if r.Header.Get("X-Amz-Target") != "" {
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_ = json.NewEncoder(w).Encode(map[string]any{"QueueUrls": urls})
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	_, _ = io.WriteString(w, xml.Header+`<ListQueuesResponse><ListQueuesResult>`)
	for _, u := range urls {
		_, _ = fmt.Fprintf(w, "<QueueUrl>%s</QueueUrl>", u)
	}
	_, _ = io.WriteString(w, `</ListQueuesResult></ListQueuesResponse>`)
}

// --- SNS: topic is the last segment of TopicArn, or Name ---

func (a *awsRouter) serveSNS(w http.ResponseWriter, r *http.Request, rt awsRoute) {
	body := readBody(r)
	if awsAction(r, body) == "ListTopics" {
		a.listTopics(w, r, rt)
		return
	}
	topic := ""
	if arn := awsParam(r, body, "TopicArn"); arn != "" {
		if i := strings.LastIndexByte(arn, ':'); i >= 0 {
			topic = arn[i+1:]
		}
	} else {
		topic = awsParam(r, body, "Name")
	}
	target, ok := rt.Resources[topic]
	if !ok {
		awsError(w, r, "NotFound", "Topic does not exist", http.StatusNotFound)
		return
	}
	a.proxyTo(target).ServeHTTP(w, r)
}

func (a *awsRouter) listTopics(w http.ResponseWriter, r *http.Request, rt awsRoute) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = io.WriteString(w, xml.Header+`<ListTopicsResponse><ListTopicsResult><Topics>`)
	for name := range rt.Resources {
		_, _ = fmt.Fprintf(w, "<member><TopicArn>arn:aws:sns:us-east-1:%s:%s</TopicArn></member>", awsAccountID, name)
	}
	_, _ = io.WriteString(w, `</Topics></ListTopicsResult></ListTopicsResponse>`)
}

// awsError writes an error in the caller's protocol (JSON or query/XML).
func awsError(w http.ResponseWriter, r *http.Request, code, msg string, status int) {
	if r.Header.Get("X-Amz-Target") != "" {
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"__type": code, "message": msg})
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "%s<ErrorResponse><Error><Code>%s</Code><Message>%s</Message></Error></ErrorResponse>", xml.Header, code, msg)
}

// buildAWSRoutes derives the per-stack AWS endpoints (s3./sqs./sns.<stack>) from
// the declared built-in instances, points each type-host at the ingress
// (127.0.0.1) in the resolver, and returns the shared route table to publish.
func (d *Daemon) buildAWSRoutes(plan *bindPlan) map[string]awsRoute {
	stack := d.cfg.Stack()
	pid := os.Getpid()
	routes := map[string]awsRoute{}
	for _, decl := range d.cfg.Instances {
		if !decl.Enabled || !awsTypes[decl.Type] {
			continue
		}
		target := plan.bind[decl.Name]
		if target == "" {
			continue
		}
		host := decl.Type + "." + stack + "." + config.DomainSuffix
		rt, ok := routes[host]
		if !ok {
			rt = awsRoute{Engine: decl.Type, Resources: map[string]string{}, PID: pid}
		}
		// Every real resource this backend serves routes to it. The engine's
		// inventory reports the wire-accurate names — a FIFO queue's `.fifo`
		// suffix and any dead-letter companion (`<name>-dlq[.fifo]`) — which the
		// bare instance name can't capture (and which real AWS requires: a FIFO
		// queue is addressable only as `<name>.fifo`). These names also feed the
		// synthesized List operations, so the aliases must be the real ones — we
		// fall back to the bare instance name only when an engine has no inventory.
		names := awsResourceNames(decl)
		if len(names) > 0 {
			for _, name := range names {
				rt.Resources[name] = target
			}
		} else {
			rt.Resources[decl.Name] = target
		}
		routes[host] = rt
		// Record the primary resource's full, directly-addressable path for the
		// dash's detail card (queue URL, bucket URL, topic ARN).
		if d.resources != nil {
			d.resources[decl.Name] = awsResourceURL(decl.Type, host, primaryResource(names, decl.Name))
		}
		// The type-host resolves to the ingress (127.0.0.1:80, wildcard bind).
		plan.resolve[host] = net.IPv4(127, 0, 0, 1)
	}
	return routes
}

// awsRegion is the fixed region the built-in AWS services present (matching the
// AWS_REGION doze exports); the local services ignore it, but ARNs need one.
const awsRegion = "us-east-1"

// primaryResource picks the main resource from an instance's wire names — the one
// that isn't the dead-letter companion (`<name>-dlq[.fifo]`) — falling back to the
// instance name when inventory gave nothing.
func primaryResource(names []string, instName string) string {
	for _, n := range names {
		if !strings.Contains(n, "-dlq") {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return instName
}

// awsResourceURL formats a resource's full, directly-addressable path: a queue
// URL, a path-style bucket URL, or a topic ARN.
func awsResourceURL(engineType, host, resource string) string {
	switch engineType {
	case "sqs":
		return "http://" + host + "/" + awsAccountID + "/" + resource
	case "sns":
		return "arn:aws:sns:" + awsRegion + ":" + awsAccountID + ":" + resource
	default: // s3: path-style bucket URL
		return "http://" + host + "/" + resource
	}
}

// awsResourceNames asks the engine's inventory for the wire-accurate resource
// names an instance serves — a FIFO queue's `.fifo` suffix, a dead-letter
// companion — which the daemon can't derive from the opaque plugin spec itself.
// Empty if the engine exposes no inventory or the plugin call fails; the caller
// still routes the bare instance name.
func awsResourceNames(decl *config.InstanceDecl) []string {
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return nil
	}
	inv, ok := drv.(engine.Inventory)
	if !ok {
		return nil
	}
	inst := engine.Instance{Name: decl.Name, Type: decl.Type, Version: decl.Version, Spec: decl.Spec}
	var names []string
	for _, o := range inv.Objects(inst) {
		if o.Name != "" {
			names = append(names, o.Name)
		}
	}
	return names
}
