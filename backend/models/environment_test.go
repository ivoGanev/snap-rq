package models

import (
	"reflect"
	"testing"
)

var testEnvironment = Environment{Variables: []EnvironmentVariable{
	{Key: "HOST", Value: "eu.example.com"},
	{Key: "EMPTY", Value: ""},
	{Key: "TOKEN", Value: "s3cr3t", Secret: true},
	{Key: "LOCKED", Secret: true, Locked: true},
}}

func TestValueTellsSetUnsetAndLockedApart(t *testing.T) {
	for _, tc := range []struct {
		key                   string
		value                 string
		found, locked, usable bool
	}{
		{"HOST", "eu.example.com", true, false, true},
		{"EMPTY", "", true, false, true},
		{"TOKEN", "s3cr3t", true, false, true},
		{"LOCKED", "", true, true, false},
		{"MISSING", "", false, false, false},
	} {
		value, found, locked := testEnvironment.Value(tc.key)
		if value != tc.value || found != tc.found || locked != tc.locked {
			t.Errorf("Value(%s) = %q, %v, %v; want %q, %v, %v", tc.key, value, found, locked, tc.value, tc.found, tc.locked)
		}
		if _, ok := testEnvironment.Get(tc.key); ok != tc.usable {
			t.Errorf("Get(%s) usable = %v, want %v (scripts see a locked secret as unset)", tc.key, ok, tc.usable)
		}
	}
}

func TestSecretValuesAreTheOpenedSecretsOnly(t *testing.T) {
	if got := testEnvironment.SecretValues(); !reflect.DeepEqual(got, []string{"s3cr3t"}) {
		t.Errorf("SecretValues = %v, want only the opened secret", got)
	}
	if got := (Environment{}).SecretValues(); len(got) != 0 {
		t.Errorf("no environment has secret values %v", got)
	}
}

func TestParseFormFields(t *testing.T) {
	fields, err := ParseFormFields(`[{"key":"a","type":"text","value":"1","enabled":true},
		{"key":"f","type":"file","file_path":"/tmp/x","file_name":"x","enabled":false}]`)
	if err != nil {
		t.Fatal(err)
	}
	want := []FormField{
		{Key: "a", Type: "text", Value: "1", Enabled: true},
		{Key: "f", Type: "file", FilePath: "/tmp/x", FileName: "x"},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("fields = %+v", fields)
	}
	if fields, err := ParseFormFields("  "); err != nil || fields != nil {
		t.Errorf("an empty body = %v, %v; want no fields", fields, err)
	}
	if _, err := ParseFormFields("{not json"); err == nil {
		t.Error("invalid JSON parsed")
	}
}
