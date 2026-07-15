package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidSetupUsernameCountsCharactersAndUTF8Bytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
		want     bool
	}{
		{name: "two multibyte letters", username: "аб", want: false},
		{name: "three multibyte letters", username: "абв", want: true},
		{name: "sixty four ascii bytes", username: strings.Repeat("a", 64), want: true},
		{name: "more than sixty four utf8 bytes", username: strings.Repeat("я", 33), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validSetupUsername(tc.username); got != tc.want {
				t.Fatalf("validSetupUsername(%q) = %v, want %v", tc.username, got, tc.want)
			}
		})
	}
}

func TestValidatePanelPasswordUsesCharactersAndBcryptByteLimit(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "eleven ascii characters", password: strings.Repeat("a", 11), wantErr: errPanelPasswordTooShort},
		{name: "six multibyte characters", password: strings.Repeat("😀", 6), wantErr: errPanelPasswordTooShort},
		{name: "twelve multibyte characters", password: strings.Repeat("😀", 12)},
		{name: "seventy two bytes", password: strings.Repeat("a", 72)},
		{name: "seventy three bytes", password: strings.Repeat("a", 73), wantErr: errPanelPasswordTooLong},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePanelPassword(tc.password)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validatePanelPassword() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validatePanelPassword() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSetupCompleteRejectsCredentialOutsidePolicy(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "username character minimum",
			body: `{"username":"аб","password":"a-long-secure-password","backupAcknowledged":true}`,
			want: "username must be 3-64 characters",
		},
		{
			name: "password character minimum",
			body: `{"username":"admin","password":"` + strings.Repeat("😀", 6) + `","backupAcknowledged":true}`,
			want: errPanelPasswordTooShort.Error(),
		},
		{
			name: "password bcrypt byte maximum",
			body: `{"username":"admin","password":"` + strings.Repeat("a", 73) + `","backupAcknowledged":true}`,
			want: errPanelPasswordTooLong.Error(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := newTestSetupState(t, true)
			req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			state.handleSetupComplete(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("response %q does not contain %q", rec.Body.String(), tc.want)
			}
		})
	}
}

func TestUserRouteGuardRejectsCredentialOutsidePolicy(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "username character minimum",
			body: `{"username":"аб","password":"a-long-secure-password","role":"viewer","locale":"en"}`,
			want: "username must be 3-64 characters",
		},
		{
			name: "password character minimum",
			body: `{"username":"bob","password":"` + strings.Repeat("😀", 6) + `","role":"viewer","locale":"en"}`,
			want: errPanelPasswordTooShort.Error(),
		},
		{
			name: "password bcrypt byte maximum",
			body: `{"username":"bob","password":"` + strings.Repeat("a", 73) + `","role":"viewer","locale":"en"}`,
			want: errPanelPasswordTooLong.Error(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := userRouteTestState(t)
			req := adminUserRouteRequest(http.MethodPost, "/api/users", tc.body)
			rec := httptest.NewRecorder()

			state.handleUsersRouteWithAdminInvariant(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("response %q does not contain %q", rec.Body.String(), tc.want)
			}
			if len(state.users) != 1 {
				t.Fatalf("invalid credential request mutated users: %+v", state.users)
			}
		})
	}
}
