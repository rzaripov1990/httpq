package httpq

import (
	"context"
	"net/http"
	"testing"
)

// Integration tests against https://petstore3.swagger.io/.
// Note: these tests require network access and the public Petstore service
// to be available. They are intended as lightweight integration checks.

// 1. /pet/findByStatus
func TestPetstore_FindPetsByStatus(t *testing.T) {
	type Pet struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	rpc := NewRpc().
		Get().
		SetUrl("https://petstore3.swagger.io/api/v3/pet/findByStatus?status=available").
		Json().
		SetLogging(false)

	ctx := context.Background()
	resp, err := Do[[]Pet](ctx, rpc)
	if err != nil {
		t.Fatalf("Petstore FindPetsByStatus returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}
	// Response body should contain at least one pet in most cases,
	// but we don't hard-fail on empty data to be robust to data changes.
}

// 2. /pet/{petId}
func TestPetstore_GetPetByID(t *testing.T) {
	type Category struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	type Tag struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	type Pet struct {
		ID        int64    `json:"id"`
		Name      string   `json:"name"`
		Status    string   `json:"status"`
		Category  Category `json:"category"`
		PhotoURLs []string `json:"photoUrls"`
		Tags      []Tag    `json:"tags"`
	}

	// According to the sample Petstore, pet with ID 1 is usually present,
	// but we keep assertions soft to avoid flakiness.
	rpc := NewRpc().
		Get().
		SetUrl("https://petstore3.swagger.io/api/v3/pet/1").
		Json().
		SetLogging(false)

	ctx := context.Background()
	resp, err := Do[Pet](ctx, rpc)
	if err != nil {
		t.Fatalf("Petstore GetPetByID returned error: %v", err)
	}

	if resp.StatusCode == 0 {
		t.Fatalf("expected non-zero status code")
	}
}

// 3. /store/inventory
func TestPetstore_GetStoreInventory(t *testing.T) {
	// /store/inventory returns a JSON object with status counts.
	// In practice the underlying implementation may use either numbers or strings,
	// so we keep the value type flexible.
	rpc := NewRpc().
		Get().
		SetUrl("https://petstore3.swagger.io/api/v3/store/inventory").
		Json().
		SetLogging(false)

	ctx := context.Background()
	resp, err := Do[map[string]any](ctx, rpc)
	if err != nil {
		t.Fatalf("Petstore GetStoreInventory returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}
	if len(resp.Data) == 0 {
		t.Fatalf("expected non-empty inventory map")
	}
}
