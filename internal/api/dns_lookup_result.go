package api

type DNSLookupResult struct {
	Hostname  string
	Addresses []string
	CNAME     string
	Err       error
}

func NewDNSLookupResult(hostname string, addresses []string, cname string, err error) DNSLookupResult {
	return DNSLookupResult{Hostname: hostname, Addresses: addresses, CNAME: cname, Err: err}
}

func (r DNSLookupResult) Map() map[string]any {
	addresses := r.Addresses
	if addresses == nil {
		addresses = []string{}
	}
	result := map[string]any{
		"hostname":  r.Hostname,
		"addresses": addresses,
	}
	if r.CNAME != "" {
		result["cname"] = r.CNAME
	}
	if r.Err != nil {
		result["error"] = r.Err.Error()
	}
	return result
}
