package identity

import (
	"context"

	"mspengine/internal/platform/identity/repo"
)

// Reencrypt rewrites every two-step verification secret not yet under the current encryption key,
// and returns how many it changed. A row changed since it was read is left for the next run.
func (s *Service) Reencrypt(ctx context.Context) (int, error) {
	changed := 0
	users, err := repo.New(s.pools.App).EncryptedUserFactors(ctx)
	if err != nil {
		return 0, err
	}
	for _, f := range users {
		if !s.keys.NeedsReencrypt(f.SecretCiphertext) {
			continue
		}
		next, err := s.reseal(f.SecretCiphertext, f.UserID[:])
		if err != nil {
			return changed, err
		}
		n, err := repo.New(s.pools.App).ReencryptUserFactor(ctx, repo.ReencryptUserFactorParams{ID: f.ID, NewCiphertext: next, KeyID: strPtr(s.keys.ID()), OldCiphertext: f.SecretCiphertext})
		if err != nil {
			return changed, err
		}
		changed += int(n)
	}
	admins, err := repo.New(s.pools.Platform).EncryptedAdminFactors(ctx)
	if err != nil {
		return changed, err
	}
	for _, f := range admins {
		if !s.keys.NeedsReencrypt(f.SecretCiphertext) {
			continue
		}
		next, err := s.reseal(f.SecretCiphertext, f.AdminID[:])
		if err != nil {
			return changed, err
		}
		n, err := repo.New(s.pools.Platform).ReencryptAdminFactor(ctx, repo.ReencryptAdminFactorParams{ID: f.ID, NewCiphertext: next, KeyID: strPtr(s.keys.ID()), OldCiphertext: f.SecretCiphertext})
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
