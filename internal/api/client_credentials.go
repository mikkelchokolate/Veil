package api

type ClientCredential struct {
	Name     string
	Username string
	Password string
}

func BuildClientCredentials(inbound Inbound) ([]ClientCredential, error) {
	return NewInboundCredentialPolicy(nil).ClientCredentials(inbound)
}
