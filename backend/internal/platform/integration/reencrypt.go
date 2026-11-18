package integration

import (
	"context"
	"errors"

	"mspengine/internal/platform/integration/repo"
)

// Reencrypt rewrites every provider credential and webhook secret not yet under the current
// encryption key, and returns how many it changed. A row changed since it was read is left for the
// next run.
func (s *Service) Reencrypt(ctx context.Context) (int, error) {
	q := repo.New(s.pools.Platform)
	if n, err := q.EncryptedConnectionCount(ctx); err != nil {
		return 0, err
	} else if n > 0 {
		return 0, errors.New("tenant connections hold secrets that key rotation does not cover yet")
	}
	changed := 0
	creds, err := q.EncryptedCredentials(ctx)
	if err != nil {
		return 0, err
	}
	for _, c := range creds {
		if !s.keys.NeedsReencrypt(c.SecretCiphertext) {
			continue
		}
		next, err := s.reseal(c.SecretCiphertext, credAAD(c.ProviderID, c.Label))
		if err != nil {
			return changed, err
		}
		n, err := q.ReencryptCredential(ctx, repo.ReencryptCredentialParams{ID: c.ID, NewCiphertext: next, KeyID: s.keys.ID(), OldCiphertext: c.SecretCiphertext})
		if err != nil {
			return changed, err
		}
		changed += int(n)
	}
	eps, err := q.EncryptedWebhookEndpoints(ctx)
	if err != nil {
		return changed, err
	}
	for _, e := range eps {
		if !s.keys.NeedsReencrypt(e.SecretCiphertext) {
			continue
		}
		next, err := s.reseal(e.SecretCiphertext, []byte(e.PathToken))
		if err != nil {
			return changed, err
		}
		n, err := q.ReencryptWebhookEndpoint(ctx, repo.ReencryptWebhookEndpointParams{ID: e.ID, NewCiphertext: next, KeyID: s.keys.ID(), OldCiphertext: e.SecretCiphertext})
		if err != nil {
			return changed, err
		}
		changed += int(n)
	}
	return changed, nil
}

func (s *Service) reseal(ciphertext, additional []byte) ([]byte, error) {
	plain, err := s.keys.Decrypt(ciphertext, additional)
	if err != nil {
		return nil, err
	}
	return s.keys.Encrypt(plain, additional)
}
