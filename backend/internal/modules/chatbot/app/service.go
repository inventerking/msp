// Package app is the Chatbot's application service: bots that answer website visitors from the
// tenant's own knowledge, capture leads with consent, and hand over to a person.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/modules/chatbot/repo"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/shared"
)

// Module is the entitlement key.
const Module = "chatbot"

// TaskAnswer is the AI task that answers visitors.
const TaskAnswer = "chatbot.answer"

// DefaultDisclosure tells visitors they are talking to software.
const DefaultDisclosure = "You are chatting with an automated assistant. Answers can be wrong. A member of our team can take over at any time."

// Deps are the services the Chatbot uses.
type Deps struct {
	Pools      *db.Pools
	Keys       crypto.Keys
	Commercial *commercial.Service
	Shared     *shared.Service
	AI         *aigateway.Gateway
	Registry   crm.Registry
}

// Service implements the Chatbot.
type Service struct{ Deps }

// New creates the service and registers its AI task.
func New(d Deps) *Service {
	if d.AI != nil {
		d.AI.RegisterTask(aigateway.Task{Key: TaskAnswer, Module: Module, ReserveCredits: 1})
	}
	return &Service{Deps: d}
}

func (s *Service) tx(ctx context.Context, p authctx.Principal, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func (s *Service) systemTx(ctx context.Context, tenantID uuid.UUID, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	return err
}

func userAudit(p authctx.Principal, action, resource string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: action, ResourceType: resource, ResourceID: id, After: after}
}

func randomToken(prefix string) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// Persona is how the bot presents itself.
type Persona struct {
	BusinessName string `json:"business_name"`
	Greeting     string `json:"greeting"`
}

// BotInput is a new bot or the fields to change.
type BotInput struct {
	Name           *string
	AllowedOrigins *[]string
	Persona        *Persona
	Disclosure     *string
	Status         *string
}

func origins(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, o := range in {
		o = strings.TrimRight(strings.ToLower(strings.TrimSpace(o)), "/")
		h, ok := strings.CutPrefix(o, "https://")
		if !ok {
			h, ok = strings.CutPrefix(o, "http://")
		}
		if !ok || h == "" || strings.ContainsAny(h, "/?#@ ") {
			return nil, apperr.Validation(apperr.FieldError{Field: "allowed_origins", Code: "invalid_origin"})
		}
		out = append(out, o)
	}
	return out, nil
}

// CreateBot adds a bot in draft. It answers no one until it is activated.
func (s *Service) CreateBot(ctx context.Context, p authctx.Principal, in BotInput) (repo.GetBotRow, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return repo.GetBotRow{}, apperr.Validation(apperr.FieldError{Field: "name", Code: "required"})
	}
	allowed := []string{}
	if in.AllowedOrigins != nil {
		var err error
		if allowed, err = origins(*in.AllowedOrigins); err != nil {
			return repo.GetBotRow{}, err
		}
	}
	persona := Persona{BusinessName: *in.Name}
	if in.Persona != nil {
		persona = *in.Persona
	}
	pj, _ := json.Marshal(persona)
	disclosure := DefaultDisclosure
	if in.Disclosure != nil && strings.TrimSpace(*in.Disclosure) != "" {
		disclosure = *in.Disclosure
	}
	key, err := randomToken("cb_")
	if err != nil {
		return repo.GetBotRow{}, err
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if id, err = q.InsertBot(ctx, repo.InsertBotParams{TenantID: p.TenantID, Name: *in.Name, PublicKey: key, AllowedOrigins: allowed, Persona: pj, DisclosureText: disclosure}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "chatbot.bot_created", "bot", &id, map[string]any{"name": *in.Name}))
	})
	if err != nil {
		return repo.GetBotRow{}, err
	}
	return s.Bot(ctx, p, id)
}

// Bot returns one bot.
func (s *Service) Bot(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetBotRow, error) {
	var out repo.GetBotRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetBot(ctx, repo.GetBotParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// Bots lists bots.
func (s *Service) Bots(ctx context.Context, p authctx.Principal) ([]repo.ListBotsRow, error) {
	var out []repo.ListBotsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListBots(ctx, p.TenantID)
		return err
	})
	return out, err
}

// UpdateBot changes a bot. Activating needs at least one allowed origin and the disclosure text.
func (s *Service) UpdateBot(ctx context.Context, p authctx.Principal, id uuid.UUID, in BotInput) (repo.GetBotRow, error) {
	var allowed []string
	if in.AllowedOrigins != nil {
		var err error
		if allowed, err = origins(*in.AllowedOrigins); err != nil {
			return repo.GetBotRow{}, err
		}
	}
	var pj []byte
	if in.Persona != nil {
		pj, _ = json.Marshal(in.Persona)
	}
	if in.Disclosure != nil && strings.TrimSpace(*in.Disclosure) == "" {
		return repo.GetBotRow{}, apperr.Validation(apperr.FieldError{Field: "disclosure_text", Code: "required"})
	}
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.UpdateBot(ctx, repo.UpdateBotParams{TenantID: p.TenantID, ID: id, Name: in.Name, AllowedOrigins: allowed, Persona: pj, DisclosureText: in.Disclosure, Status: in.Status})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		b, err := q.GetBot(ctx, repo.GetBotParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if b.Status == "active" && len(b.AllowedOrigins) == 0 {
			return apperr.Validation(apperr.FieldError{Field: "allowed_origins", Code: "required_to_activate"})
		}
		return audit.Record(ctx, tx, userAudit(p, "chatbot.bot_updated", "bot", &id, map[string]any{"status": in.Status, "allowed_origins": allowed}))
	})
	if err != nil {
		return repo.GetBotRow{}, err
	}
	return s.Bot(ctx, p, id)
}

// DeleteBot removes a bot. Its key stops working at once; conversations stay for the record.
func (s *Service) DeleteBot(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteBot(ctx, repo.DeleteBotParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "chatbot.bot_deleted", "bot", &id, nil))
	})
}

// Stats counts a bot's conversations, leads and handoffs.
func (s *Service) Stats(ctx context.Context, p authctx.Principal, bot uuid.UUID) (repo.BotStatsRow, error) {
	var out repo.BotStatsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetBot(ctx, repo.GetBotParams{TenantID: p.TenantID, ID: bot}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.BotStats(ctx, repo.BotStatsParams{TenantID: p.TenantID, BotID: bot})
		return err
	})
	return out, err
}

// Knowledge.

// Embedding dimensions of the fake embedder. A real embedding model replaces it after D-05.
const embedDims = 64

// embed is a deterministic bag-of-words vector, so retrieval can be built and tested without an
// embedding provider. It returns pgvector's text form.
func embed(text string) string {
	v := make([]float64, embedDims)
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') }) {
		if len(w) < 3 {
			continue
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[h.Sum32()%embedDims]++
	}
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		v[0], norm = 1, 1 // A zero vector has no direction; give empty text a fixed one.
	}
	parts := make([]string, embedDims)
	for i, x := range v {
		parts[i] = strconv.FormatFloat(x/norm, 'f', 6, 64)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// chunks splits text into pieces of about 800 characters, on paragraph boundaries where possible.
func chunks(text string) []string {
	var out []string
	var cur strings.Builder
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		for len(para) > 800 {
			cut := strings.LastIndex(para[:800], " ")
			if cut < 200 {
				cut = 800
			}
			out = append(out, strings.TrimSpace(para[:cut]))
			para = strings.TrimSpace(para[cut:])
		}
		if cur.Len() > 0 && cur.Len()+len(para) > 800 {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// AddKnowledge adds text the bot may answer from. Knowledge is the tenant's own content, and only
// this bot ever retrieves it.
func (s *Service) AddKnowledge(ctx context.Context, p authctx.Principal, bot uuid.UUID, kind, title, content string) (uuid.UUID, error) {
	if kind != "faq" && kind != "document" && kind != "service_catalog" {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "kind", Code: "invalid"})
	}
	pieces := chunks(content)
	if len(pieces) == 0 || len(pieces) > 200 {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "content", Code: "invalid_length"})
	}
	sum := sha256.Sum256([]byte(content))
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetBot(ctx, repo.GetBotParams{TenantID: p.TenantID, ID: bot}); err != nil {
			return notFound(err)
		}
		var err error
		if id, err = q.InsertSource(ctx, repo.InsertSourceParams{TenantID: p.TenantID, BotID: bot, Kind: kind, Title: title, ContentHash: sum[:]}); err != nil {
			return err
		}
		for i, c := range pieces {
			if err := q.InsertChunk(ctx, repo.InsertChunkParams{TenantID: p.TenantID, BotID: bot, SourceID: id, Position: int32(i), Content: c, Embedding: embed(c), TokenCount: int32(len(c) / 4)}); err != nil { //nolint:gosec // At most 200 chunks of 800 characters.
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "chatbot.knowledge_added", "knowledge_source", &id, map[string]any{"title": title, "chunks": len(pieces)}))
	})
	return id, err
}

// Knowledge lists a bot's sources.
func (s *Service) Knowledge(ctx context.Context, p authctx.Principal, bot uuid.UUID) ([]repo.ListSourcesRow, error) {
	var out []repo.ListSourcesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetBot(ctx, repo.GetBotParams{TenantID: p.TenantID, ID: bot}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.ListSources(ctx, repo.ListSourcesParams{TenantID: p.TenantID, BotID: bot})
		return err
	})
	return out, err
}

// DeleteKnowledge removes a source and its chunks, so the bot stops using it at once.
func (s *Service) DeleteKnowledge(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteSource(ctx, repo.DeleteSourceParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		if err := q.DeleteSourceChunks(ctx, repo.DeleteSourceChunksParams{TenantID: p.TenantID, SourceID: id}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "chatbot.knowledge_removed", "knowledge_source", &id, nil))
	})
}

// Qualification questions.

// Question is one qualification question.
type Question struct {
	FieldKey, Question string
	Weight             int32
	Required           bool
}

// Questions lists a bot's qualification questions.
func (s *Service) Questions(ctx context.Context, p authctx.Principal, bot uuid.UUID) ([]repo.ListRulesRow, error) {
	var out []repo.ListRulesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetBot(ctx, repo.GetBotParams{TenantID: p.TenantID, ID: bot}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.ListRules(ctx, repo.ListRulesParams{TenantID: p.TenantID, BotID: bot})
		return err
	})
	return out, err
}

// SetQuestions replaces a bot's qualification questions.
func (s *Service) SetQuestions(ctx context.Context, p authctx.Principal, bot uuid.UUID, qs []Question) error {
	seen := map[string]bool{}
	for _, x := range qs {
		if seen[x.FieldKey] {
			return apperr.Validation(apperr.FieldError{Field: "questions", Code: "duplicate_field_key"})
		}
		seen[x.FieldKey] = true
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetBot(ctx, repo.GetBotParams{TenantID: p.TenantID, ID: bot}); err != nil {
			return notFound(err)
		}
		if err := q.ClearRules(ctx, repo.ClearRulesParams{TenantID: p.TenantID, BotID: bot}); err != nil {
			return err
		}
		for i, x := range qs {
			if err := q.InsertRule(ctx, repo.InsertRuleParams{TenantID: p.TenantID, BotID: bot, Position: int32(i), FieldKey: x.FieldKey, Question: x.Question, Weight: x.Weight, Required: x.Required}); err != nil { //nolint:gosec // Bounded by the contract.
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "chatbot.questions_set", "bot", &bot, map[string]any{"count": len(qs)}))
	})
}
