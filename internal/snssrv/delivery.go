package snssrv

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/nerdmenot/doze/internal/awslocal"
)

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sns: "+format+"\n", args...)
}

// snsNotification is the JSON envelope SNS delivers (non-raw subscriptions).
type snsNotification struct {
	Type              string                `json:"Type"`
	MessageID         string                `json:"MessageId"`
	TopicARN          string                `json:"TopicArn"`
	Subject           string                `json:"Subject,omitempty"`
	Message           string                `json:"Message"`
	Timestamp         string                `json:"Timestamp"`
	MessageAttributes map[string]notifyAttr `json:"MessageAttributes,omitempty"`
}

type notifyAttr struct {
	Type  string `json:"Type"`
	Value string `json:"Value"`
}

func envelope(msgID, topicARN, subject, message string, attrs map[string]Attr) snsNotification {
	n := snsNotification{
		Type: "Notification", MessageID: msgID, TopicARN: topicARN,
		Subject: subject, Message: message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if len(attrs) > 0 {
		n.MessageAttributes = map[string]notifyAttr{}
		for k, a := range attrs {
			na := notifyAttr{Type: a.DataType, Value: a.StringValue}
			if len(a.BinaryValue) > 0 {
				na.Value = base64.StdEncoding.EncodeToString(a.BinaryValue)
			}
			n.MessageAttributes[k] = na
		}
	}
	return n
}

// deliver fans a published message out to every confirmed, filter-matching
// subscription of the topic. Delivery is synchronous (simpler and deterministic
// for local dev).
func (srv *server) deliver(msgID, topicARN, subject, message string, attrs map[string]Attr) {
	subs, err := srv.store.subsForTopic(topicARN)
	if err != nil {
		logf("deliver: %v", err)
		return
	}
	for _, sub := range subs {
		if !matchFilter(sub.FilterPolicy, attrs) {
			continue
		}
		switch sub.Protocol {
		case "sqs":
			srv.deliverSQS(sub, msgID, topicARN, subject, message, attrs)
		case "http", "https":
			srv.deliverHTTP(sub, msgID, topicARN, subject, message, attrs)
		}
	}
}

func (srv *server) deliverSQS(sub Subscription, msgID, topicARN, subject, message string, attrs map[string]Attr) {
	if srv.sqsSocket == "" {
		logf("subscription %s targets SQS but no SQS backend is wired (set `sqs = \"<instance>\"` on the sns block)", sub.ARN)
		return
	}
	queue := lastSegment(sub.Endpoint)
	payload := map[string]any{"QueueUrl": "http://sqs/" + awslocal.AccountID + "/" + queue}
	if sub.RawDelivery {
		payload["MessageBody"] = message
		if sqsAttrs := toSQSAttrs(attrs); sqsAttrs != nil {
			payload["MessageAttributes"] = sqsAttrs
		}
	} else {
		body, _ := json.Marshal(envelope(msgID, topicARN, subject, message, attrs))
		payload["MessageBody"] = string(body)
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "http://unix/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonSQS.SendMessage")
	resp, err := awslocal.UnixHTTPClient(srv.sqsSocket).Do(req)
	if err != nil {
		logf("deliver to sqs %q: %v", queue, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		logf("deliver to sqs %q: status %s", queue, resp.Status)
	}
}

// toSQSAttrs converts SNS message attributes to the SQS SendMessage JSON shape.
func toSQSAttrs(attrs map[string]Attr) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, a := range attrs {
		m := map[string]string{"DataType": a.DataType}
		if len(a.BinaryValue) > 0 {
			m["BinaryValue"] = base64.StdEncoding.EncodeToString(a.BinaryValue)
		} else {
			m["StringValue"] = a.StringValue
		}
		out[k] = m
	}
	return out
}

var httpDeliveryClient = &http.Client{Timeout: 5 * time.Second}

func (srv *server) deliverHTTP(sub Subscription, msgID, topicARN, subject, message string, attrs map[string]Attr) {
	body, _ := json.Marshal(envelope(msgID, topicARN, subject, message, attrs))
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		logf("deliver to %s: %v", sub.Endpoint, err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=UTF-8")
	req.Header.Set("x-amz-sns-message-type", "Notification")
	resp, err := httpDeliveryClient.Do(req)
	if err != nil {
		logf("deliver to %s: %v", sub.Endpoint, err)
		return
	}
	_ = resp.Body.Close()
}

// sendConfirmation posts a SubscriptionConfirmation to an http(s) endpoint so it
// can confirm by fetching SubscribeURL (or calling ConfirmSubscription).
func (srv *server) sendConfirmation(sub Subscription, host string) {
	subscribeURL := fmt.Sprintf("http://%s/?Action=ConfirmSubscription&TopicArn=%s&Token=%s",
		host, url.QueryEscape(sub.TopicARN), url.QueryEscape(sub.Token))
	payload, _ := json.Marshal(map[string]string{
		"Type":         "SubscriptionConfirmation",
		"TopicArn":     sub.TopicARN,
		"Token":        sub.Token,
		"Message":      "You have chosen to subscribe to the topic " + sub.TopicARN,
		"SubscribeURL": subscribeURL,
		"Timestamp":    time.Now().UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(payload))
	if err != nil {
		logf("confirmation to %s: %v", sub.Endpoint, err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=UTF-8")
	req.Header.Set("x-amz-sns-message-type", "SubscriptionConfirmation")
	resp, err := httpDeliveryClient.Do(req)
	if err != nil {
		logf("confirmation to %s: %v", sub.Endpoint, err)
		return
	}
	_ = resp.Body.Close()
}
