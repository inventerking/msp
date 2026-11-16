-- Bots, as the application role under the tenant's context.

-- name: InsertBot :one
INSERT INTO chatbot.bots (tenant_id, name, public_key, allowed_origins, persona, disclosure_text, status)
VALUES (@tenant_id, @name, @public_key, @allowed_origins, @persona, @disclosure_text, 'draft') RETURNING id;

-- name: GetBot :one
SELECT id, name, public_key, allowed_origins, persona, disclosure_text, status, created_at, updated_at FROM chatbot.bots WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListBots :many
SELECT id, name, public_key, allowed_origins, persona, disclosure_text, status, created_at, updated_at FROM chatbot.bots WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY created_at;

-- name: UpdateBot :execrows
UPDATE chatbot.bots SET name = coalesce(sqlc.narg(name), name), allowed_origins = coalesce(sqlc.narg(allowed_origins), allowed_origins), persona = coalesce(sqlc.narg(persona), persona),
  disclosure_text = coalesce(sqlc.narg(disclosure_text), disclosure_text), status = coalesce(sqlc.narg(status), status)
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteBot :execrows
UPDATE chatbot.bots SET deleted_at = now(), status = 'paused', public_key = public_key || ':deleted:' || id WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: BotByKey :one
-- Runs as the system role, which may read active bots only, to learn a public key's tenant.
SELECT id, tenant_id FROM chatbot.bots WHERE public_key = @public_key AND status = 'active' AND deleted_at IS NULL;

-- Knowledge.

-- name: InsertSource :one
INSERT INTO chatbot.knowledge_sources (tenant_id, bot_id, kind, title, content_hash, status) VALUES (@tenant_id, @bot_id, @kind, @title, @content_hash, 'ready') RETURNING id;

-- name: ListSources :many
SELECT s.id, s.kind, s.title, s.status, s.version, s.created_at, (SELECT count(*) FROM chatbot.knowledge_chunks c WHERE c.tenant_id = s.tenant_id AND c.source_id = s.id)::int AS chunks
FROM chatbot.knowledge_sources s WHERE s.tenant_id = @tenant_id AND s.bot_id = @bot_id AND s.deleted_at IS NULL ORDER BY s.created_at;

-- name: DeleteSource :execrows
UPDATE chatbot.knowledge_sources SET deleted_at = now(), status = 'disabled' WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteSourceChunks :exec
DELETE FROM chatbot.knowledge_chunks WHERE tenant_id = @tenant_id AND source_id = @source_id;

-- name: InsertChunk :exec
INSERT INTO chatbot.knowledge_chunks (tenant_id, bot_id, source_id, position, content, embedding, token_count)
VALUES (@tenant_id, @bot_id, @source_id, @position, @content, sqlc.arg(embedding)::vector, @token_count);

-- name: SearchChunks :many
-- Only this tenant's bot's own knowledge, nearest first.
SELECT id, content, (embedding <=> sqlc.arg(query)::vector)::float8 AS distance FROM chatbot.knowledge_chunks
WHERE tenant_id = @tenant_id AND bot_id = @bot_id ORDER BY embedding <=> sqlc.arg(query)::vector LIMIT 4;

-- Qualification rules.

-- name: ListRules :many
SELECT id, position, field_key, question, weight, required FROM chatbot.qualification_rules WHERE tenant_id = @tenant_id AND bot_id = @bot_id AND deleted_at IS NULL ORDER BY position;

-- name: ClearRules :exec
UPDATE chatbot.qualification_rules SET deleted_at = now() WHERE tenant_id = @tenant_id AND bot_id = @bot_id AND deleted_at IS NULL;

-- name: InsertRule :exec
INSERT INTO chatbot.qualification_rules (tenant_id, bot_id, position, field_key, question, weight, required) VALUES (@tenant_id, @bot_id, @position, @field_key, @question, @weight, @required);

-- Conversations.

-- name: InsertConversation :one
INSERT INTO chatbot.conversations (tenant_id, bot_id, session_token_hash, status, last_message_at) VALUES (@tenant_id, @bot_id, @session_token_hash, 'active', now()) RETURNING id;

-- name: ConversationByToken :one
SELECT id, status, contact_id FROM chatbot.conversations WHERE tenant_id = @tenant_id AND bot_id = @bot_id AND session_token_hash = @session_token_hash FOR UPDATE;

-- name: GetConversation :one
SELECT c.id, c.bot_id, c.status, c.contact_id, c.created_at, c.last_message_at, b.name AS bot_name FROM chatbot.conversations c
JOIN chatbot.bots b ON b.tenant_id = c.tenant_id AND b.id = c.bot_id WHERE c.tenant_id = @tenant_id AND c.id = @id;

-- name: ListConversations :many
SELECT c.id, c.bot_id, c.status, c.contact_id, c.created_at, c.last_message_at, b.name AS bot_name,
  (SELECT count(*) FROM chatbot.messages m WHERE m.tenant_id = c.tenant_id AND m.conversation_id = c.id)::int AS messages
FROM chatbot.conversations c JOIN chatbot.bots b ON b.tenant_id = c.tenant_id AND b.id = c.bot_id
WHERE c.tenant_id = @tenant_id AND (sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text) ORDER BY c.last_message_at DESC NULLS LAST LIMIT 200;

-- name: SetConversationStatus :execrows
UPDATE chatbot.conversations SET status = @status, last_message_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: LinkConversationContact :exec
UPDATE chatbot.conversations SET contact_id = @contact_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertMessage :one
INSERT INTO chatbot.messages (tenant_id, conversation_id, role, content, retrieved_chunk_ids, safety_flags, ai_request_id, sender_user_id)
VALUES (@tenant_id, @conversation_id, @role, @content, @retrieved_chunk_ids, @safety_flags, sqlc.narg(ai_request_id), sqlc.narg(sender_user_id)) RETURNING id, created_at;

-- name: ListMessages :many
SELECT id, role, content, safety_flags, ai_request_id, sender_user_id, created_at FROM chatbot.messages
WHERE tenant_id = @tenant_id AND conversation_id = @conversation_id ORDER BY created_at, id LIMIT 500;

-- name: CountVisitorMessages :one
SELECT count(*)::int FROM chatbot.messages WHERE tenant_id = @tenant_id AND conversation_id = @conversation_id AND role = 'visitor';

-- name: TouchConversation :exec
UPDATE chatbot.conversations SET last_message_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- Qualification, handoffs.

-- name: UpsertQualification :exec
INSERT INTO chatbot.qualifications (tenant_id, conversation_id, answers, score, status, crm_company_id) VALUES (@tenant_id, @conversation_id, @answers, @score, @status, sqlc.narg(crm_company_id))
ON CONFLICT (tenant_id, conversation_id) DO UPDATE SET answers = EXCLUDED.answers, score = EXCLUDED.score, status = EXCLUDED.status, crm_company_id = coalesce(EXCLUDED.crm_company_id, chatbot.qualifications.crm_company_id);

-- name: GetQualification :one
SELECT answers, score, status, crm_company_id FROM chatbot.qualifications WHERE tenant_id = @tenant_id AND conversation_id = @conversation_id;

-- name: InsertHandoff :one
INSERT INTO chatbot.handoffs (tenant_id, conversation_id, reason, status) VALUES (@tenant_id, @conversation_id, @reason, 'requested') RETURNING id;

-- name: OpenHandoff :one
SELECT id FROM chatbot.handoffs WHERE tenant_id = @tenant_id AND conversation_id = @conversation_id AND status IN ('requested', 'accepted') LIMIT 1;

-- name: ListHandoffs :many
SELECT h.id, h.conversation_id, h.reason, h.status, h.assigned_user_id, h.created_at, b.name AS bot_name FROM chatbot.handoffs h
JOIN chatbot.conversations c ON c.tenant_id = h.tenant_id AND c.id = h.conversation_id JOIN chatbot.bots b ON b.tenant_id = c.tenant_id AND b.id = c.bot_id
WHERE h.tenant_id = @tenant_id AND h.status IN ('requested', 'accepted') ORDER BY h.created_at;

-- name: AcceptHandoffs :exec
UPDATE chatbot.handoffs SET status = 'accepted', assigned_user_id = @user_id WHERE tenant_id = @tenant_id AND conversation_id = @conversation_id AND status = 'requested';

-- name: ResolveHandoffs :exec
UPDATE chatbot.handoffs SET status = 'resolved', resolved_at = now() WHERE tenant_id = @tenant_id AND conversation_id = @conversation_id AND status IN ('requested', 'accepted');

-- Erasure.

-- name: RedactContactConversations :exec
UPDATE chatbot.messages m SET content = '[erased]' FROM chatbot.conversations c
WHERE c.tenant_id = m.tenant_id AND c.id = m.conversation_id AND m.tenant_id = @tenant_id AND c.contact_id = @contact_id AND m.role = 'visitor';

-- name: RedactContactQualifications :exec
UPDATE chatbot.qualifications q SET answers = '{}' FROM chatbot.conversations c
WHERE c.tenant_id = q.tenant_id AND c.id = q.conversation_id AND q.tenant_id = @tenant_id AND c.contact_id = @contact_id;

-- name: UnlinkContactConversations :exec
UPDATE chatbot.conversations SET contact_id = NULL WHERE tenant_id = @tenant_id AND contact_id = @contact_id;

-- name: ContactConversations :many
SELECT id, created_at FROM chatbot.conversations WHERE tenant_id = @tenant_id AND contact_id = @contact_id;

-- name: BotStats :one
SELECT
  (SELECT count(*) FROM chatbot.conversations c WHERE c.tenant_id = @tenant_id AND c.bot_id = @bot_id)::int AS conversations,
  (SELECT count(*) FROM chatbot.conversations c WHERE c.tenant_id = @tenant_id AND c.bot_id = @bot_id AND c.contact_id IS NOT NULL)::int AS leads,
  (SELECT count(*) FROM chatbot.handoffs h JOIN chatbot.conversations c ON c.tenant_id = h.tenant_id AND c.id = h.conversation_id WHERE h.tenant_id = @tenant_id AND c.bot_id = @bot_id)::int AS handoffs;
