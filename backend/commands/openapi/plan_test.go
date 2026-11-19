package openapi

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

const sampleSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Pet Store", "version": "1.0.0"},
  "servers": [{"url": "https://api.example.com/v1"}],
  "tags": [{"name": "pets"}, {"name": "store"}],
  "paths": {
    "/pets/{petId}": {
      "get": {
        "operationId": "getPet",
        "tags": ["pets"],
        "parameters": [
          {"name": "petId", "in": "path", "required": true, "schema": {"type": "integer"}},
          {"name": "verbose", "in": "query", "schema": {"type": "boolean"}}
        ]
      },
      "post": {
        "operationId": "createPet",
        "tags": ["pets"],
        "requestBody": {
          "content": {
            "application/json": {"example": {"name": "Rex"}}
          }
        }
      }
    },
    "/store/orders": {
      "get": {"operationId": "listOrders", "tags": ["store"]}
    },
    "/health": {
      "get": {"operationId": "health"}
    }
  }
}`

func TestBuildPlanGroupsByTag(t *testing.T) {
	loader := openapi3.Loader{}
	doc, err := loader.LoadFromData([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("loading spec: %v", err)
	}

	plan := buildPlan(doc)
	if len(plan.Collections) != 3 {
		t.Fatalf("expected 3 collections (pets, store, untagged title), got %d", len(plan.Collections))
	}

	byName := map[string]collectionSeed{}
	for _, c := range plan.Collections {
		byName[c.Name] = c
	}

	pets := byName["pets"]
	if len(pets.Requests) != 2 {
		t.Fatalf("expected 2 pets requests, got %d", len(pets.Requests))
	}
	// Sorted by URL then method: createPet (POST) sorts before getPet (GET).
	if pets.Requests[0].Name != "createPet" || pets.Requests[0].Method != "POST" {
		t.Fatalf("pets[0] = %s %s, want createPet POST", pets.Requests[0].Name, pets.Requests[0].Method)
	}
	if pets.Requests[0].URL != "https://api.example.com/v1/pets/{{petId}}" {
		t.Fatalf("pets[0] url = %q", pets.Requests[0].URL)
	}
	if pets.Requests[0].BodyType != "json" || pets.Requests[0].Body == "" {
		t.Fatalf("pets[0] body_type = %q body = %q", pets.Requests[0].BodyType, pets.Requests[0].Body)
	}
	if pets.Requests[1].Name != "getPet" || pets.Requests[1].Method != "GET" {
		t.Fatalf("pets[1] = %s %s, want getPet GET", pets.Requests[1].Name, pets.Requests[1].Method)
	}
	if pets.Requests[1].URL != "https://api.example.com/v1/pets/{{petId}}?verbose={{verbose}}" {
		t.Fatalf("pets[1] url = %q", pets.Requests[1].URL)
	}

	store := byName["store"]
	if len(store.Requests) != 1 || store.Requests[0].Name != "listOrders" {
		t.Fatalf("store requests = %+v", store.Requests)
	}

	untagged := byName["Pet Store"]
	if len(untagged.Requests) != 1 || untagged.Requests[0].Name != "health" {
		t.Fatalf("untagged requests = %+v", untagged.Requests)
	}
}
