package snssrv

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	bolt "go.etcd.io/bbolt"

	"github.com/nerdmenot/doze/internal/sqssrv"
)

var creds = credentials.NewStaticCredentialsProvider("test", "test", "")

// TestSNSSDKFanout drives the real aws-sdk-go-v2 SNS + SQS clients: SNS→SQS
// fanout (raw delivery), filter policies, and an HTTP webhook subscription with
// the confirmation handshake.
func TestSNSSDKFanout(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// SQS server on a unix socket (the fanout target).
	sqsSock := filepath.Join(dir, "sqs.sock")
	sqsData := filepath.Join(dir, "sqsdata")
	if err := os.MkdirAll(sqsData, 0o700); err != nil {
		t.Fatal(err)
	}
	sqsHandler, sqsCloser, err := sqssrv.New(sqsData)
	if err != nil {
		t.Fatal(err)
	}
	defer sqsCloser.Close()
	ln, err := net.Listen("unix", sqsSock)
	if err != nil {
		t.Fatal(err)
	}
	sqsHTTP := &http.Server{Handler: sqsHandler}
	go sqsHTTP.Serve(ln)
	defer sqsHTTP.Close()
	sqsClient := sqsOverUnix(sqsSock)

	// SNS server (httptest, reachable by the SDK) wired to the SQS socket.
	snsDB, err := bolt.Open(filepath.Join(dir, "sns.bolt"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer snsDB.Close()
	ts := httptest.NewServer(&server{store: newStore(snsDB), sqsSocket: sqsSock})
	defer ts.Close()
	snsClient := sns.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: creds},
		func(o *sns.Options) { o.BaseEndpoint = aws.String(ts.URL) })

	t.Run("fanout to SQS (raw delivery)", func(t *testing.T) {
		qURL := createQueue(t, ctx, sqsClient, "emails")
		qARN := queueARNOf(t, ctx, sqsClient, qURL)
		topic := createTopic(t, ctx, snsClient, "events")

		subscribe(t, ctx, snsClient, topic, "sqs", qARN, map[string]string{"RawMessageDelivery": "true"})
		publish(t, ctx, snsClient, topic, "hello fanout", nil)

		got := receiveOne(t, ctx, sqsClient, qURL)
		if got != "hello fanout" {
			t.Fatalf("fanout body = %q, want %q", got, "hello fanout")
		}
	})

	t.Run("filter policy", func(t *testing.T) {
		qURL := createQueue(t, ctx, sqsClient, "filtered")
		qARN := queueARNOf(t, ctx, sqsClient, qURL)
		topic := createTopic(t, ctx, snsClient, "filter-topic")
		subscribe(t, ctx, snsClient, topic, "sqs", qARN, map[string]string{
			"RawMessageDelivery": "true",
			"FilterPolicy":       `{"eventType":["created"]}`,
		})

		attr := func(v string) map[string]snstypes.MessageAttributeValue {
			return map[string]snstypes.MessageAttributeValue{
				"eventType": {DataType: aws.String("String"), StringValue: aws.String(v)},
			}
		}
		publish(t, ctx, snsClient, topic, "nope", attr("deleted"))  // filtered out
		publish(t, ctx, snsClient, topic, "yes", attr("created"))   // matches

		got := receiveOne(t, ctx, sqsClient, qURL)
		if got != "yes" {
			t.Fatalf("filter delivered %q, want only %q", got, "yes")
		}
	})

	t.Run("http webhook with confirmation", func(t *testing.T) {
		received := make(chan map[string]any, 4)
		webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			received <- body
			w.WriteHeader(http.StatusOK)
		}))
		defer webhook.Close()

		topic := createTopic(t, ctx, snsClient, "webhook-topic")
		subscribe(t, ctx, snsClient, topic, "http", webhook.URL, nil)

		conf := waitMsg(t, received)
		if conf["Type"] != "SubscriptionConfirmation" {
			t.Fatalf("expected SubscriptionConfirmation, got %v", conf["Type"])
		}
		// Confirm by fetching SubscribeURL, as a real endpoint would.
		resp, err := http.Get(conf["SubscribeURL"].(string))
		if err != nil {
			t.Fatalf("confirm: %v", err)
		}
		resp.Body.Close()

		publish(t, ctx, snsClient, topic, "ping", nil)
		note := waitMsg(t, received)
		if note["Type"] != "Notification" || note["Message"] != "ping" {
			t.Fatalf("webhook notification wrong: %v", note)
		}
	})
}

// ---- helpers ----

func sqsOverUnix(sock string) *sqs.Client {
	hc := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	return sqs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: creds},
		func(o *sqs.Options) { o.BaseEndpoint = aws.String("http://sqs"); o.HTTPClient = hc })
}

func createQueue(t *testing.T, ctx context.Context, c *sqs.Client, name string) string {
	t.Helper()
	out, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name)})
	if err != nil {
		t.Fatalf("CreateQueue %s: %v", name, err)
	}
	return aws.ToString(out.QueueUrl)
}

func queueARNOf(t *testing.T, ctx context.Context, c *sqs.Client, qurl string) string {
	t.Helper()
	out, err := c.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(qurl), AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		t.Fatalf("GetQueueAttributes: %v", err)
	}
	return out.Attributes["QueueArn"]
}

func receiveOne(t *testing.T, ctx context.Context, c *sqs.Client, qurl string) string {
	t.Helper()
	out, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl: aws.String(qurl), MaxNumberOfMessages: 1, WaitTimeSeconds: 3,
	})
	if err != nil || len(out.Messages) == 0 {
		t.Fatalf("ReceiveMessage: %v, %d msgs", err, len(out.Messages))
	}
	return aws.ToString(out.Messages[0].Body)
}

func createTopic(t *testing.T, ctx context.Context, c *sns.Client, name string) string {
	t.Helper()
	out, err := c.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(name)})
	if err != nil {
		t.Fatalf("CreateTopic %s: %v", name, err)
	}
	return aws.ToString(out.TopicArn)
}

func subscribe(t *testing.T, ctx context.Context, c *sns.Client, topic, proto, endpoint string, attrs map[string]string) {
	t.Helper()
	_, err := c.Subscribe(ctx, &sns.SubscribeInput{
		TopicArn: aws.String(topic), Protocol: aws.String(proto), Endpoint: aws.String(endpoint),
		Attributes: attrs, ReturnSubscriptionArn: true,
	})
	if err != nil {
		t.Fatalf("Subscribe %s->%s: %v", proto, endpoint, err)
	}
}

func publish(t *testing.T, ctx context.Context, c *sns.Client, topic, msg string, attrs map[string]snstypes.MessageAttributeValue) {
	t.Helper()
	_, err := c.Publish(ctx, &sns.PublishInput{TopicArn: aws.String(topic), Message: aws.String(msg), MessageAttributes: attrs})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func waitMsg(t *testing.T, ch chan map[string]any) map[string]any {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
		return nil
	}
}
