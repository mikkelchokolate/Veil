package api

func (s *managementState) hashPassword(password []byte) ([]byte, error) {
	hasher := s.passwordHasher
	if hasher == nil {
		hasher = productionPasswordHasher()
	}
	return hasher.Hash(password)
}
