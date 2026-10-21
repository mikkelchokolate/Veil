package clientaccess

import "testing"

func TestClientAccessDeliveryArtifactFilenameFallsBackToProtocol(t *testing.T) {
	filename := clientArtifactFilename(ClientLink{Name: "", Protocol: "mieru"})
	if filename != "mieru.json" {
		t.Fatalf("filename = %q, want mieru.json", filename)
	}
}

// Issue #1277: credential names are not charset-bounded, so the artifact
// filename must not carry quotes, separators or control runes that would
// break out of a Content-Disposition filename parameter.
func TestClientAccessDeliveryArtifactFilenameStripsHeaderUnsafeRunes(t *testing.T) {
	tests := []struct {
		name string
		link ClientLink
		want string
	}{
		{"plain", ClientLink{Name: "alice phone", Protocol: "naive"}, "alice-phone.json"},
		{"quote and semicolon", ClientLink{Name: `a"b;c`, Protocol: "naive"}, "a-b-c.json"},
		{"crlf injection", ClientLink{Name: "x\r\nSet-Cookie: s=1", Protocol: "naive"}, "xSet-Cookie:-s=1.json"},
		{"backslash", ClientLink{Name: `..\..\evil`, Protocol: "naive"}, "..-..-evil.json"},
		{"bidi override", ClientLink{Name: "doc\u202Etxt", Protocol: "naive"}, "doctxt.json"},
		{"control-only name falls back to protocol", ClientLink{Name: "\x00\x07", Protocol: "hysteria2"}, "hysteria2.json"},
		{"nothing usable", ClientLink{Name: "\x00", Protocol: ""}, "veil-subscription.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clientArtifactFilename(tc.link)
			if got != tc.want {
				t.Fatalf("clientArtifactFilename(%q) = %q, want %q", tc.link.Name, got, tc.want)
			}
			for _, r := range got {
				if r < 0x20 || r == 0x7f || r == '"' || r == ';' || r == '\\' || r == '/' {
					t.Fatalf("filename %q contains header-unsafe rune %q", got, r)
				}
			}
		})
	}
}
