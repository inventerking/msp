package aigateway

import (
	"context"
	"encoding/json"
)

// FakeProvider sends nothing outside the system (D-05). It returns a fixed reply that fits the
// platform.summarize schema unless told otherwise, and tests can script it.
type FakeProvider struct {
	// Reply, if set, decides the response to each request.
	Reply func(ProviderRequest) (ProviderResponse, error)
}

// Key names the provider.
func (*FakeProvider) Key() string { return "fake_ai" }

// Complete returns a fixed or scripted reply.
func (f *FakeProvider) Complete(_ context.Context, r ProviderRequest) (ProviderResponse, error) {
	if f.Reply != nil {
		return f.Reply(r)
	}
	b, err := json.Marshal(map[string]string{"summary": "Fake summary. No AI provider is connected."})
	if err != nil {
		return ProviderResponse{}, err
	}
	return ProviderResponse{Text: string(b), InputTokens: int32(len(r.User) / 4), OutputTokens: 20}, nil //nolint:gosec // Test data.
}
