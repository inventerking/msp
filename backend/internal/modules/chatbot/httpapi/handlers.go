// Package httpapi serves the Chatbot's contract. The route guard has already authenticated,
// checked the entitlement and permission, and validated the request. Public chat routes carry no
// session: the bot's key and the page's origin decide what they may do.
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/chatbot/app"
	"mspengine/internal/modules/chatbot/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func ok(w http.ResponseWriter, status int, v any) { httpx.JSON(w, status, v) }

func done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func botOut(b repo.GetBotRow, stats *repo.BotStatsRow) Bot {
	var p Persona
	_ = json.Unmarshal(b.Persona, &p)
	origins := b.AllowedOrigins
	if origins == nil {
		origins = []string{}
	}
	out := Bot{Id: b.ID, Name: b.Name, PublicKey: b.PublicKey, AllowedOrigins: origins, Persona: p, DisclosureText: b.DisclosureText, Status: b.Status, CreatedAt: b.CreatedAt}
	if stats != nil {
		out.Stats = &BotStats{Conversations: int(stats.Conversations), Leads: int(stats.Leads), Handoffs: int(stats.Handoffs)}
	}
	return out
}

func persona(p *Persona) *app.Persona {
	if p == nil {
		return nil
	}
	out := app.Persona{}
	if p.BusinessName != nil {
		out.BusinessName = *p.BusinessName
	}
	if p.Greeting != nil {
		out.Greeting = *p.Greeting
	}
	return &out
}

// ListBots handles GET /chatbot/bots.
func (h Handlers) ListBots(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Bots(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := BotList{Data: []Bot{}}
	for _, b := range rows {
		out.Data = append(out.Data, botOut(repo.GetBotRow(b), nil))
	}
	ok(w, 200, out)
}

// CreateBot handles POST /chatbot/bots.
func (h Handlers) CreateBot(w http.ResponseWriter, r *http.Request) {
	var b BotInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	bot, err := h.S.CreateBot(r.Context(), principal(r), app.BotInput{Name: &b.Name, AllowedOrigins: b.AllowedOrigins, Persona: persona(b.Persona), Disclosure: b.DisclosureText})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, botOut(bot, nil))
}

// GetBot handles GET /chatbot/bots/{id}.
func (h Handlers) GetBot(w http.ResponseWriter, r *http.Request, id Id) {
	b, err := h.S.Bot(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	st, err := h.S.Stats(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, botOut(b, &st))
}

// UpdateBot handles PATCH /chatbot/bots/{id}.
func (h Handlers) UpdateBot(w http.ResponseWriter, r *http.Request, id Id) {
	var b BotPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	var status *string
	if b.Status != nil {
		s := string(*b.Status)
		status = &s
	}
	bot, err := h.S.UpdateBot(r.Context(), principal(r), id, app.BotInput{Name: b.Name, AllowedOrigins: b.AllowedOrigins, Persona: persona(b.Persona), Disclosure: b.DisclosureText, Status: status})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, botOut(bot, nil))
}

// DeleteBot handles DELETE /chatbot/bots/{id}.
func (h Handlers) DeleteBot(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.DeleteBot(r.Context(), principal(r), id))
}

// ListKnowledge handles GET /chatbot/bots/{id}/knowledge.
func (h Handlers) ListKnowledge(w http.ResponseWriter, r *http.Request, id Id) {
	rows, err := h.S.Knowledge(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := KnowledgeList{Data: []Knowledge{}}
	for _, k := range rows {
		out.Data = append(out.Data, Knowledge{Id: k.ID, Kind: k.Kind, Title: k.Title, Status: k.Status, Chunks: int(k.Chunks), CreatedAt: k.CreatedAt})
	}
	ok(w, 200, out)
}

// AddKnowledge handles POST /chatbot/bots/{id}/knowledge.
func (h Handlers) AddKnowledge(w http.ResponseWriter, r *http.Request, id Id) {
	var b KnowledgeInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	kid, err := h.S.AddKnowledge(r.Context(), principal(r), id, string(b.Kind), b.Title, b.Content)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, Ref{Id: kid})
}

// DeleteKnowledge handles DELETE /chatbot/knowledge/{id}.
func (h Handlers) DeleteKnowledge(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.DeleteKnowledge(r.Context(), principal(r), id))
}

// ListQuestions handles GET /chatbot/bots/{id}/questions.
func (h Handlers) ListQuestions(w http.ResponseWriter, r *http.Request, id Id) {
	rows, err := h.S.Questions(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := QuestionList{Data: []Question{}}
	for _, q := range rows {
		out.Data = append(out.Data, Question{FieldKey: q.FieldKey, Question: q.Question, Weight: int(q.Weight), Required: q.Required})
	}
	ok(w, 200, out)
}

// SetQuestions handles PUT /chatbot/bots/{id}/questions.
func (h Handlers) SetQuestions(w http.ResponseWriter, r *http.Request, id Id) {
	var b QuestionsInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	qs := make([]app.Question, len(b.Questions))
	for i, q := range b.Questions {
		qs[i] = app.Question{FieldKey: q.FieldKey, Question: q.Question, Weight: int32(q.Weight), Required: q.Required} //nolint:gosec // Bounded by the contract.
	}
	done(w, r, h.S.SetQuestions(r.Context(), principal(r), id, qs))
}

// ListConversations handles GET /chatbot/conversations.
func (h Handlers) ListConversations(w http.ResponseWriter, r *http.Request, params ListConversationsParams) {
	var status *string
	if params.Status != nil {
		s := string(*params.Status)
		status = &s
	}
	rows, err := h.S.Conversations(r.Context(), principal(r), status)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ConversationList{Data: []Conversation{}}
	for _, c := range rows {
		out.Data = append(out.Data, Conversation{Id: c.ID, BotId: c.BotID, BotName: c.BotName, Status: c.Status, ContactId: c.ContactID, CreatedAt: c.CreatedAt, LastMessageAt: c.LastMessageAt, Messages: int(c.Messages)})
	}
	ok(w, 200, out)
}

// GetConversation handles GET /chatbot/conversations/{id}.
func (h Handlers) GetConversation(w http.ResponseWriter, r *http.Request, id Id) {
	v, err := h.S.Conversation(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ConversationDetail{Id: v.ID, BotId: v.BotID, BotName: v.BotName, Status: v.Status, ContactId: v.ContactID, CreatedAt: v.CreatedAt, Transcript: []TranscriptMessage{}}
	for _, m := range v.Messages {
		flags := m.SafetyFlags
		if flags == nil {
			flags = []string{}
		}
		ai := m.AiRequestID != nil
		out.Transcript = append(out.Transcript, TranscriptMessage{Id: m.ID, Role: m.Role, Content: m.Content, SafetyFlags: flags, AiGenerated: &ai, SenderUserId: m.SenderUserID, CreatedAt: m.CreatedAt})
	}
	if q := v.Qualification; q != nil {
		answers := map[string]string{}
		_ = json.Unmarshal(q.Answers, &answers)
		var score *int
		if q.Score != nil {
			n := int(*q.Score)
			score = &n
		}
		out.Qualification = &Qualification{Answers: answers, Score: score, Status: q.Status, CrmCompanyId: q.CrmCompanyID}
	}
	ok(w, 200, out)
}

// ReplyToConversation handles POST /chatbot/conversations/{id}/reply.
func (h Handlers) ReplyToConversation(w http.ResponseWriter, r *http.Request, id Id) {
	var b ReplyInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	done(w, r, h.S.Reply(r.Context(), principal(r), id, b.Message))
}

// CloseConversation handles POST /chatbot/conversations/{id}/close.
func (h Handlers) CloseConversation(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.Close(r.Context(), principal(r), id))
}

// ListHandoffs handles GET /chatbot/handoffs.
func (h Handlers) ListHandoffs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Handoffs(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := HandoffList{Data: []Handoff{}}
	for _, x := range rows {
		out.Data = append(out.Data, Handoff{Id: x.ID, ConversationId: x.ConversationID, BotName: x.BotName, Reason: x.Reason, Status: x.Status, AssignedUserId: x.AssignedUserID, CreatedAt: x.CreatedAt})
	}
	ok(w, 200, out)
}

// origin is the page's origin. Browsers leave out the Origin header on same-origin GET requests,
// so the origin part of the Referer stands in for it then.
func origin(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" {
		return o
	}
	if u, err := url.Parse(r.Referer()); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return ""
}

func messagesOut(ms []app.Message) []ChatMessage {
	out := make([]ChatMessage, 0, len(ms))
	for _, m := range ms {
		out = append(out, ChatMessage{Id: m.ID, Role: m.Role, Content: m.Content, CreatedAt: m.At})
	}
	return out
}

// GetChatConfig handles GET /public/chat/{key}/config.
func (h Handlers) GetChatConfig(w http.ResponseWriter, r *http.Request, key Key) {
	c, err := h.S.Config(r.Context(), key, origin(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ChatConfig{Name: c.Name, Disclosure: c.Disclosure, Greeting: c.Greeting, ConsentText: c.ConsentText, Questions: []ChatQuestion{}}
	for _, q := range c.Questions {
		out.Questions = append(out.Questions, ChatQuestion{FieldKey: q.FieldKey, Question: q.Question, Required: q.Required})
	}
	ok(w, 200, out)
}

// StartChat handles POST /public/chat/{key}/conversations.
func (h Handlers) StartChat(w http.ResponseWriter, r *http.Request, key Key) {
	token, err := h.S.Start(r.Context(), key, origin(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	ms, err := h.S.Poll(r.Context(), key, origin(r), token)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, ChatStart{Token: token, Messages: messagesOut(ms)})
}

// SendChatMessage handles POST /public/chat/{key}/messages.
func (h Handlers) SendChatMessage(w http.ResponseWriter, r *http.Request, key Key) {
	var b ChatMessageInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	ms, err := h.S.Send(r.Context(), key, origin(r), b.Token, b.Message)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, ChatMessages{Messages: messagesOut(ms)})
}

// PollChat handles POST /public/chat/{key}/poll.
func (h Handlers) PollChat(w http.ResponseWriter, r *http.Request, key Key) {
	var b ChatTokenInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	ms, err := h.S.Poll(r.Context(), key, origin(r), b.Token)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, ChatMessages{Messages: messagesOut(ms)})
}

// CaptureChatLead handles POST /public/chat/{key}/lead.
func (h Handlers) CaptureChatLead(w http.ResponseWriter, r *http.Request, key Key) {
	var b LeadInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	l := app.Lead{Name: b.Name, Email: string(b.Email), MarketingConsent: b.MarketingConsent, Answers: map[string]string{}}
	if b.Company != nil {
		l.Company = *b.Company
	}
	if b.PreferredTime != nil {
		l.PreferredTime = *b.PreferredTime
	}
	if b.Answers != nil {
		l.Answers = *b.Answers
	}
	done(w, r, h.S.CaptureLead(r.Context(), key, origin(r), b.Token, l))
}
