package installer

import "testing"

func TestBinaryDownloadRequestValidationRequiresURLDestinationAndChecksum(t *testing.T) {
	validator := NewBinaryDownloadRequestValidation()
	cases := []struct {
		name string
		req  DownloadRequest
		want string
	}{
		{"missing url", DownloadRequest{Destination: "/bin/x", SHA256: "abc"}, "download url is required"},
		{"missing destination", DownloadRequest{URL: "https://example.com/x", SHA256: "abc"}, "download destination is required"},
		{"missing checksum", DownloadRequest{URL: "https://example.com/x", Destination: "/bin/x"}, "sha256 checksum is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validator.Normalize(tc.req)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBinaryDownloadRequestValidationDefaultsMode(t *testing.T) {
	req, err := NewBinaryDownloadRequestValidation().Normalize(DownloadRequest{URL: "https://example.com/x", Destination: "/bin/x", SHA256: "abc"})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if req.Mode != 0o755 {
		t.Fatalf("mode = %o", req.Mode)
	}
}
