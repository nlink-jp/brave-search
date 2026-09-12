package brave

import (
	"context"
	"testing"
)

func TestProbeSendsAnInvalidRequestAndReportsTheRefusal(t *testing.T) {
	c, cap := serve(t, 422, `{"error":{"code":"VALIDATION","detail":"q is required"}}`, nil)
	meta, err := c.Probe(context.Background(), EndpointWebSearch)
	if Code(err) != CodeInvalidArguments || meta.Status != 422 {
		t.Errorf("err=%v meta=%+v", err, meta)
	}
	if cap.method != "GET" || len(cap.query) != 0 {
		t.Errorf("probe sent %s with %v", cap.method, cap.query)
	}

	c, cap = serve(t, 401, `{}`, nil)
	c.AnswersAPIKey = "ak"
	if _, err := c.Probe(context.Background(), EndpointAnswers); Code(err) != CodeUnauthorized {
		t.Errorf("err = %v", err)
	}
	if cap.method != "POST" || cap.header.Get("X-Subscription-Token") != "ak" {
		t.Errorf("probe sent %s with token %q", cap.method, cap.header.Get("X-Subscription-Token"))
	}

	c, _ = serve(t, 200, `{}`, nil)
	if meta, err := c.Probe(context.Background(), EndpointWebSearch); err != nil || meta.Status != 200 {
		t.Errorf("2xx: err=%v meta=%+v", err, meta)
	}

	c.APIKey = ""
	if _, err := c.Probe(context.Background(), EndpointWebSearch); Code(err) != CodeMissingAPIKey {
		t.Errorf("no key: err = %v", err)
	}
}
