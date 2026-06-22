package snssrv

import (
	"fmt"
	"net/url"
)

// dispatch maps an SNS action to its handler.
var dispatch = map[string]func(*server, url.Values, string) (any, *apiError){
	"CreateTopic":             (*server).createTopic,
	"DeleteTopic":             (*server).deleteTopic,
	"ListTopics":              (*server).listTopics,
	"GetTopicAttributes":      (*server).getTopicAttributes,
	"Subscribe":               (*server).subscribe,
	"ConfirmSubscription":     (*server).confirmSubscription,
	"Unsubscribe":             (*server).unsubscribe,
	"ListSubscriptions":       (*server).listSubscriptions,
	"ListSubscriptionsByTopic": (*server).listSubscriptionsByTopic,
	"SetSubscriptionAttributes": (*server).setSubscriptionAttributes,
	"Publish":                 (*server).publish,
	"PublishBatch":            (*server).publishBatch,
}

func asErr(err error) *apiError {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*apiError); ok {
		return ae
	}
	return &apiError{Code: "InternalError", Status: 500, Msg: err.Error()}
}

// ---- result shapes (XML member-wrapped, per SNS Query protocol) ----

type createTopicResult struct {
	TopicArn string `xml:"TopicArn"`
}
type subscribeResult struct {
	SubscriptionArn string `xml:"SubscriptionArn"`
}
type confirmResult struct {
	SubscriptionArn string `xml:"SubscriptionArn"`
}
type publishResult struct {
	MessageID string `xml:"MessageId"`
}
type topicMember struct {
	TopicArn string `xml:"TopicArn"`
}
type listTopicsResult struct {
	Topics struct {
		Member []topicMember `xml:"member"`
	} `xml:"Topics"`
}
type subMember struct {
	SubscriptionArn string `xml:"SubscriptionArn"`
	Owner           string `xml:"Owner"`
	Protocol        string `xml:"Protocol"`
	Endpoint        string `xml:"Endpoint"`
	TopicArn        string `xml:"TopicArn"`
}
type listSubsResult struct {
	Subscriptions struct {
		Member []subMember `xml:"member"`
	} `xml:"Subscriptions"`
}
type attrEntry struct {
	Key   string `xml:"key"`
	Value string `xml:"value"`
}
type getTopicAttrsResult struct {
	Attributes struct {
		Entry []attrEntry `xml:"entry"`
	} `xml:"Attributes"`
}
type pbSuccess struct {
	ID        string `xml:"Id"`
	MessageID string `xml:"MessageId"`
}
type publishBatchResult struct {
	Successful struct {
		Member []pbSuccess `xml:"member"`
	} `xml:"Successful"`
	Failed struct {
		Member []any `xml:"member"`
	} `xml:"Failed"`
}

// ---- handlers ----

func (srv *server) createTopic(form url.Values, _ string) (any, *apiError) {
	t, err := srv.store.CreateTopic(form.Get("Name"))
	if err != nil {
		return nil, asErr(err)
	}
	return createTopicResult{TopicArn: t.ARN}, nil
}

func (srv *server) deleteTopic(form url.Values, _ string) (any, *apiError) {
	return nil, asErr(srv.store.DeleteTopic(form.Get("TopicArn")))
}

func (srv *server) listTopics(_ url.Values, _ string) (any, *apiError) {
	topics, err := srv.store.ListTopics()
	if err != nil {
		return nil, asErr(err)
	}
	var res listTopicsResult
	for _, t := range topics {
		res.Topics.Member = append(res.Topics.Member, topicMember{TopicArn: t.ARN})
	}
	return res, nil
}

func (srv *server) getTopicAttributes(form url.Values, _ string) (any, *apiError) {
	arn := form.Get("TopicArn")
	if !srv.store.TopicExists(arn) {
		return nil, errNotFound("topic does not exist: " + arn)
	}
	subs, _ := srv.store.ListSubscriptions(arn)
	var res getTopicAttrsResult
	res.Attributes.Entry = []attrEntry{
		{Key: "TopicArn", Value: arn},
		{Key: "SubscriptionsConfirmed", Value: fmt.Sprintf("%d", countConfirmed(subs))},
		{Key: "SubscriptionsPending", Value: fmt.Sprintf("%d", len(subs)-countConfirmed(subs))},
	}
	return res, nil
}

func countConfirmed(subs []Subscription) int {
	n := 0
	for _, s := range subs {
		if s.Confirmed {
			n++
		}
	}
	return n
}

func (srv *server) subscribe(form url.Values, host string) (any, *apiError) {
	topicArn, proto, endpoint := form.Get("TopicArn"), form.Get("Protocol"), form.Get("Endpoint")
	if topicArn == "" || proto == "" || endpoint == "" {
		return nil, errInvalid("TopicArn, Protocol and Endpoint are required")
	}
	sub, err := srv.store.Subscribe(topicArn, proto, endpoint, subscribeAttributes(form))
	if err != nil {
		return nil, asErr(err)
	}
	arn := sub.ARN
	if !sub.Confirmed {
		srv.sendConfirmation(*sub, host)
		if form.Get("ReturnSubscriptionArn") != "true" {
			arn = "pending confirmation"
		}
	}
	return subscribeResult{SubscriptionArn: arn}, nil
}

func (srv *server) confirmSubscription(form url.Values, _ string) (any, *apiError) {
	sub, err := srv.store.ConfirmByToken(form.Get("Token"))
	if err != nil {
		return nil, asErr(err)
	}
	return confirmResult{SubscriptionArn: sub.ARN}, nil
}

func (srv *server) unsubscribe(form url.Values, _ string) (any, *apiError) {
	return nil, asErr(srv.store.Unsubscribe(form.Get("SubscriptionArn")))
}

func (srv *server) listSubscriptions(_ url.Values, _ string) (any, *apiError) {
	return srv.subscriptionList("")
}

func (srv *server) listSubscriptionsByTopic(form url.Values, _ string) (any, *apiError) {
	return srv.subscriptionList(form.Get("TopicArn"))
}

func (srv *server) subscriptionList(topic string) (any, *apiError) {
	subs, err := srv.store.ListSubscriptions(topic)
	if err != nil {
		return nil, asErr(err)
	}
	var res listSubsResult
	for _, s := range subs {
		arn := s.ARN
		if !s.Confirmed {
			arn = "PendingConfirmation"
		}
		res.Subscriptions.Member = append(res.Subscriptions.Member, subMember{
			SubscriptionArn: arn, Owner: "000000000000", Protocol: s.Protocol,
			Endpoint: s.Endpoint, TopicArn: s.TopicARN,
		})
	}
	return res, nil
}

func (srv *server) setSubscriptionAttributes(form url.Values, _ string) (any, *apiError) {
	return nil, asErr(srv.store.SetSubscriptionAttribute(
		form.Get("SubscriptionArn"), form.Get("AttributeName"), form.Get("AttributeValue")))
}

func (srv *server) publish(form url.Values, _ string) (any, *apiError) {
	topicArn := form.Get("TopicArn")
	if topicArn == "" {
		topicArn = form.Get("TargetArn")
	}
	if !srv.store.TopicExists(topicArn) {
		return nil, errNotFound("topic does not exist: " + topicArn)
	}
	id := newID()
	srv.deliver(id, topicArn, form.Get("Subject"), form.Get("Message"), messageAttributes(form))
	return publishResult{MessageID: id}, nil
}

func (srv *server) publishBatch(form url.Values, _ string) (any, *apiError) {
	topicArn := form.Get("TopicArn")
	if !srv.store.TopicExists(topicArn) {
		return nil, errNotFound("topic does not exist: " + topicArn)
	}
	var res publishBatchResult
	for i := 1; ; i++ {
		base := fmt.Sprintf("PublishBatchRequestEntries.member.%d.", i)
		id := form.Get(base + "Id")
		if id == "" {
			break
		}
		mid := newID()
		srv.deliver(mid, topicArn, form.Get(base+"Subject"), form.Get(base+"Message"), nil)
		res.Successful.Member = append(res.Successful.Member, pbSuccess{ID: id, MessageID: mid})
	}
	return res, nil
}
