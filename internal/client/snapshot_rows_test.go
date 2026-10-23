package client

import (
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// fillNonZero sets every exported field of the struct pointed to by v to a
// distinct non-zero value, so a converter that forgets a field is caught.
func fillNonZero(t *testing.T, v any) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Field(i)
		n := int64(i + 1)
		switch f.Kind() {
		case reflect.String:
			f.SetString(rv.Type().Field(i).Name + "-value")
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int, reflect.Int64:
			f.SetInt(n)
		case reflect.Slice:
			if f.Type().Elem().Kind() != reflect.Uint8 {
				t.Fatalf("fillNonZero: unsupported slice field %s", rv.Type().Field(i).Name)
			}
			f.SetBytes([]byte{byte(n)})
		case reflect.Pointer:
			p := reflect.New(f.Type().Elem())
			switch p.Elem().Kind() {
			case reflect.String:
				p.Elem().SetString(rv.Type().Field(i).Name + "-value")
			case reflect.Int, reflect.Int64:
				p.Elem().SetInt(n)
			default:
				t.Fatalf("fillNonZero: unsupported pointer field %s", rv.Type().Field(i).Name)
			}
			f.Set(p)
		default:
			t.Fatalf("fillNonZero: unsupported field %s (%s)", rv.Type().Field(i).Name, f.Kind())
		}
	}
}

// assertCarriesEveryField requires every field of the snapshot row to exist on
// the source row under the same name and to hold the source's value.
func assertCarriesEveryField(t *testing.T, source, snapshot any) {
	t.Helper()
	src := reflect.ValueOf(source)
	dst := reflect.ValueOf(snapshot)
	for i := 0; i < dst.NumField(); i++ {
		name := dst.Type().Field(i).Name
		from := src.FieldByName(name)
		if !from.IsValid() {
			t.Errorf("%s.%s has no source field on %s", dst.Type().Name(), name, src.Type().Name())
			continue
		}
		if dst.Field(i).IsZero() {
			t.Errorf("%s.%s was not copied (zero value)", dst.Type().Name(), name)
			continue
		}
		if !reflect.DeepEqual(from.Interface(), dst.Field(i).Interface()) {
			t.Errorf("%s.%s = %#v, want %#v", dst.Type().Name(), name, dst.Field(i).Interface(), from.Interface())
		}
	}
}

func TestSnapshotRowsCarryEverySnapshotField(t *testing.T) {
	var c Client
	var b Binding
	var k Credential
	fillNonZero(t, &c)
	fillNonZero(t, &b)
	fillNonZero(t, &k)
	clients, bindings, credentials := SnapshotRows([]Client{c}, []Binding{b}, []Credential{k})
	if len(clients) != 1 || len(bindings) != 1 || len(credentials) != 1 {
		t.Fatalf("rows = %d/%d/%d, want 1/1/1", len(clients), len(bindings), len(credentials))
	}
	assertCarriesEveryField(t, c, clients[0])
	assertCarriesEveryField(t, b, bindings[0])
	assertCarriesEveryField(t, k, credentials[0])
}

// sourceOnlyFields are repository fields that deliberately have no snapshot
// counterpart. A snapshot freezes only active credentials, so RevokedAt is
// always nil there. Every other field must be snapshotted; adding a column to
// Client/Binding/Credential without a snapshot field (or an entry here) fails
// TestSnapshotModelCoversEverySourceField.
var sourceOnlyFields = map[string]bool{
	"Credential.RevokedAt": true,
}

// TestSnapshotModelCoversEverySourceField is the reverse of
// TestSnapshotRowsCarryEverySnapshotField, which only proves the converter
// forgets no snapshot field: this one fails when a source column has no
// snapshot field at all (and is not listed in sourceOnlyFields).
func TestSnapshotModelCoversEverySourceField(t *testing.T) {
	pairs := []struct{ source, snapshot reflect.Type }{
		{reflect.TypeOf(Client{}), reflect.TypeOf(model.ClientSnapshot{})},
		{reflect.TypeOf(Binding{}), reflect.TypeOf(model.BindingSnapshot{})},
		{reflect.TypeOf(Credential{}), reflect.TypeOf(model.CredentialSnapshot{})},
	}
	for _, pair := range pairs {
		for i := 0; i < pair.source.NumField(); i++ {
			name := pair.source.Field(i).Name
			key := pair.source.Name() + "." + name
			if sourceOnlyFields[key] {
				if _, ok := pair.snapshot.FieldByName(name); ok {
					t.Errorf("%s is allowlisted as source-only but %s has it; drop the allowlist entry", key, pair.snapshot.Name())
				}
				continue
			}
			if _, ok := pair.snapshot.FieldByName(name); !ok {
				t.Errorf("%s has no field on %s: a rollback would silently drop it", key, pair.snapshot.Name())
			}
		}
	}
}

// TestRowsFromSnapshotRoundTrip pins the rollback direction: rows rebuilt
// from a snapshot equal the original rows on every snapshotted field.
func TestRowsFromSnapshotRoundTrip(t *testing.T) {
	var c Client
	var b Binding
	var k Credential
	fillNonZero(t, &c)
	fillNonZero(t, &b)
	fillNonZero(t, &k)
	snapClients, snapBindings, snapCredentials := SnapshotRows([]Client{c}, []Binding{b}, []Credential{k})
	clients, bindings, credentials := RowsFromSnapshot(snapClients, snapBindings, snapCredentials)
	wantCredential := k
	wantCredential.RevokedAt = nil // source-only; never snapshotted
	if !reflect.DeepEqual(clients, []Client{c}) {
		t.Errorf("client round trip = %+v, want %+v", clients, []Client{c})
	}
	if !reflect.DeepEqual(bindings, []Binding{b}) {
		t.Errorf("binding round trip = %+v, want %+v", bindings, []Binding{b})
	}
	if !reflect.DeepEqual(credentials, []Credential{wantCredential}) {
		t.Errorf("credential round trip = %+v, want %+v", credentials, []Credential{wantCredential})
	}
	snapCredentials[0].EncryptedValue[0]++
	if credentials[0].EncryptedValue[0] == snapCredentials[0].EncryptedValue[0] {
		t.Error("RowsFromSnapshot aliases the snapshot's EncryptedValue")
	}
}

func TestSnapshotRowsEmptyInputsYieldEmptyRows(t *testing.T) {
	clients, bindings, credentials := SnapshotRows(nil, nil, nil)
	if clients == nil || bindings == nil || credentials == nil {
		t.Fatal("SnapshotRows must return non-nil empty slices")
	}
	if len(clients)+len(bindings)+len(credentials) != 0 {
		t.Fatal("SnapshotRows of nothing returned rows")
	}
}
