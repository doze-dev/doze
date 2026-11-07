//go:build integration

package integration

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	names "github.com/doze-dev/doze-names"
)

// Module names, end to end: the kafka and aws modules, built from their own
// repositories, declare their listeners and routes (engine.Networked), and
// doze gives them names. This is the test that a client sees what the design
// promises (docs/design/next/module-names.md):
//
//   - kafka: the broker on its name and port, advertising that name; the
//     console at http://events.<stack>.doze; the schema registry at
//     http://registry.events.<stack>.doze.
//   - aws: http://aws.<stack>.doze, a bucket in front of it (an SDK's default
//     virtual-hosted S3), and sync-aws.<stack>.doze (Step Functions'
//     StartSyncExecution).
//   - Every one of those wakes a sleeping instance.
//
// It runs in a private zone (see names_test.go), so it needs no machine setup.
// The modules' sources are sibling checkouts of doze, or DOZE_AWS_SRC and
// DOZE_KAFKA_SRC.

const moduleStack = `stack = "mods"

defaults {
  names = true
}

module kafka "events" {
  version = 4
  port    = %d
}

module aws "aws" {
  port = %d
}
`

// buildModulePlugin builds a module's plugin from a sibling repository's
// module/ directory.
func buildModulePlugin(t *testing.T, repo, env string) string {
	t.Helper()
	src := os.Getenv(env)
	if src == "" {
		src = filepath.Join(repoRoot(t), "..", repo)
	}
	if _, err := os.Stat(filepath.Join(src, "module", "plugin")); err != nil {
		t.Skipf("%s not found at %s (set %s): %v", repo, src, env, err)
	}
	out := filepath.Join(t.TempDir(), repo+"-plugin")
	cmd := exec.Command("go", "build", "-o", out, "./plugin")
	cmd.Dir = filepath.Join(src, "module")
	cmd.Env = append(os.Environ(), "GOFLAGS=") // the modules have no purego tag of their own
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the %s module: %v\n%s", repo, err, b)
	}
	return out
}

// call is one HTTP request through the zone's front door, as a client that
// resolved host would make it. Redirects are answers, not something to follow.
func (z *zone) call(method, host, path string, header map[string]string, body string) (int, string, error) {
	req, err := http.NewRequest(method, "http://"+z.front+path, strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Host = host
	for k, v := range header {
		req.Header.Set(k, v)
	}
	c := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

func (z *zone) mustCall(method, host, path string, header map[string]string, body string, want int) string {
	z.t.Helper()
	code, got, err := z.call(method, host, path, header, body)
	if err != nil {
		z.t.Fatalf("%s http://%s%s: %v", method, host, path, err)
	}
	if code != want {
		z.t.Fatalf("%s http://%s%s: %d, want %d\n%s", method, host, path, code, want, got)
	}
	return got
}

// brokers asks a Kafka broker for its metadata (v0, the oldest every broker
// speaks) and returns the host:port of each broker it advertises.
func brokers(addr string) ([]string, error) {
	c, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	const clientID = "doze-test"
	req := binary.BigEndian.AppendUint16(nil, 3) // Metadata
	req = binary.BigEndian.AppendUint16(req, 0)  // v0
	req = binary.BigEndian.AppendUint32(req, 1)  // correlation id
	req = binary.BigEndian.AppendUint16(req, uint16(len(clientID)))
	req = append(req, clientID...)
	req = binary.BigEndian.AppendUint32(req, 0) // no topics
	if _, err := c.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(req))), req...)); err != nil {
		return nil, err
	}
	r := bufio.NewReader(c)
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return nil, err
	}
	resp := make([]byte, size)
	if _, err := io.ReadFull(r, resp); err != nil {
		return nil, err
	}
	// correlation id, then [node_id int32, host string, port int32]
	b := resp[4:]
	n := int(binary.BigEndian.Uint32(b))
	b = b[4:]
	var out []string
	for i := 0; i < n; i++ {
		b = b[4:]
		l := int(binary.BigEndian.Uint16(b))
		host := string(b[2 : 2+l])
		b = b[2+l:]
		port := int(binary.BigEndian.Uint32(b))
		b = b[4:]
		out = append(out, net.JoinHostPort(host, strconv.Itoa(port)))
	}
	return out, nil
}

// awakeRow is a row of doze status for an instance that is not asleep.
var awakeRow = regexp.MustCompile(`(?m)^.*\s(aws|events)\s.*\s(awake|waking|dozing)\s`)

func TestModulesAnswerAtTheirNames(t *testing.T) {
	kafkaPlugin := buildModulePlugin(t, "doze-kafka", "DOZE_KAFKA_SRC")
	awsPlugin := buildModulePlugin(t, "doze-aws", "DOZE_AWS_SRC")
	kafkaPort, awsPort := FreePort(t), FreePort(t)
	z := newZone(t, fmt.Sprintf(moduleStack, kafkaPort, awsPort))
	z.env = append(z.env, "DOZE_MODULES=on", "DOZE_TEST_KAFKA_PLUGIN="+kafkaPlugin, "DOZE_TEST_AWS_PLUGIN="+awsPlugin)
	z.mustRun("up")

	const (
		events   = "events.mods.doze"
		registry = "registry.events.mods.doze"
		aws      = "aws.mods.doze"
	)

	// What a program is told: each instance by its name.
	env := z.mustRun("env")
	for _, want := range []string{
		fmt.Sprintf("KAFKA_BROKERS='%s:%d'", events, kafkaPort),
		fmt.Sprintf("AWS_ENDPOINT_URL='http://%s'", aws),
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("doze env does not say %s:\n%s", want, env)
		}
	}

	// Every name resolves to this machine: the instances' own, and the names
	// their routes asked for.
	resolved := map[string]string{}
	for _, host := range []string{events, registry, aws, "photos." + aws, "sync-" + aws} {
		waitFor(t, host+" resolves", func() error {
			ip := z.lookup(host)
			if !names.OnThisMachine(net.ParseIP(ip)) {
				return fmt.Errorf("%s resolved to %q", host, ip)
			}
			resolved[host] = ip
			return nil
		})
	}

	// kafka: a client bootstrapped on the name is told the same name back, so
	// its next connection goes where the first one did.
	broker := net.JoinHostPort(resolved[events], strconv.Itoa(kafkaPort))
	got, err := brokers(broker)
	if err != nil {
		t.Fatalf("kafka metadata from %s: %v", broker, err)
	}
	if want := fmt.Sprintf("%s:%d", events, kafkaPort); len(got) != 1 || got[0] != want {
		t.Fatalf("the broker advertises %v, want [%s]", got, want)
	}

	// The console is the instance's own name; the registry is a child of it.
	if code, body, err := z.call("GET", events, "/", nil, ""); err != nil || code >= 400 {
		t.Fatalf("the console at http://%s: %d %v\n%.300s", events, code, err, body)
	}
	schema := `{"schema":"{\"type\":\"string\"}"}`
	z.mustCall("POST", registry, "/subjects/orders-value/versions",
		map[string]string{"Content-Type": "application/vnd.schemaregistry.v1+json"}, schema, 200)
	if body := z.mustCall("GET", registry, "/subjects", nil, "", 200); strings.TrimSpace(body) != `["orders-value"]` {
		t.Fatalf("the registry lists %s, want [\"orders-value\"]", body)
	}

	// aws: a bucket in front of the name is that bucket (an SDK's default
	// virtual-hosted addressing given AWS_ENDPOINT_URL), the name itself is
	// every other service, and sync-<name> is Step Functions' sync endpoint.
	z.mustCall("PUT", "photos."+aws, "/", nil, "", 200)
	z.mustCall("PUT", "photos."+aws, "/cat.txt", nil, "meow", 200)
	if body := z.mustCall("GET", "photos."+aws, "/cat.txt", nil, "", 200); body != "meow" {
		t.Fatalf("GET photos.%s/cat.txt = %q, want meow", aws, body)
	}
	if body := z.mustCall("GET", aws, "/", nil, "", 200); !strings.Contains(body, "<Name>photos</Name>") {
		t.Fatalf("ListBuckets at %s does not list photos:\n%s", aws, body)
	}
	sfn := func(host, op, body string) string {
		return z.mustCall("POST", host, "/", map[string]string{
			"Content-Type": "application/x-amz-json-1.0",
			"X-Amz-Target": "AWSStepFunctions." + op,
		}, body, 200)
	}
	created := sfn(aws, "CreateStateMachine", `{"name":"hello","type":"EXPRESS","roleArn":"arn:aws:iam::000000000000:role/x",`+
		`"definition":"{\"StartAt\":\"P\",\"States\":{\"P\":{\"Type\":\"Pass\",\"Result\":{\"ok\":1},\"End\":true}}}"}`)
	arn := regexp.MustCompile(`"stateMachineArn":"([^"]+)"`).FindStringSubmatch(created)
	if arn == nil {
		t.Fatalf("CreateStateMachine said %s", created)
	}
	if out := sfn("sync-"+aws, "StartSyncExecution", `{"stateMachineArn":"`+arn[1]+`","input":"{}"}`); !strings.Contains(out, `"status":"SUCCEEDED"`) {
		t.Fatalf("StartSyncExecution at sync-%s said %s", aws, out)
	}

	// Asleep, each of them wakes on what a client does first: the broker on a
	// connection to its port, the registry and S3 on a request to their names.
	for _, wake := range []struct {
		what string
		do   func() error
	}{
		{"the broker's port", func() error { _, err := brokers(broker); return err }},
		{"the registry's name", func() error {
			if code, body, err := z.call("GET", registry, "/subjects", nil, ""); err != nil || code != 200 {
				return fmt.Errorf("%d %v %s", code, err, body)
			}
			return nil
		}},
		{"a bucket's name", func() error {
			if code, body, err := z.call("GET", "photos."+aws, "/cat.txt", nil, ""); err != nil || code != 200 || body != "meow" {
				return fmt.Errorf("%d %v %q", code, err, body)
			}
			return nil
		}},
	} {
		z.mustRun("sleep")
		if status := z.mustRun("status"); awakeRow.MatchString(status) {
			t.Fatalf("after doze sleep something is still awake:\n%s", status)
		}
		if err := wake.do(); err != nil {
			t.Fatalf("a request to %s did not wake it: %v\n%s", wake.what, err, z.mustRun("status"))
		}
	}
}
