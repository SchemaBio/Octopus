package handler

import (
	"encoding/json"
	"testing"
)

func TestPersonalViewContainsOnlyQueryState(t *testing.T) {
	good := json.RawMessage(`{"searchQuery":"GENE","filters":{},"columnFilters":[{"column":"reviewed","operator":"equals","value":"false"}],"page":1,"pageSize":20,"sortDirection":"asc"}`)
	if err := validatePersonalView(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"page":1,"pageSize":20,"url":"https://signed.example"}`,
		`{"page":1,"pageSize":20,"sql":"SELECT *"}`,
		`{"page":0,"pageSize":20}`,
		`{"page":1,"pageSize":9999}`,
	} {
		if validatePersonalView(json.RawMessage(bad)) == nil {
			t.Fatal("accepted invalid personal view")
		}
	}
}
