package feishu

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Link and button parsing share a switch but must not share scoped URL state.
func TestCardExtractionLinkAndButtonIntegration(t *testing.T) {
	elements := []json.RawMessage{
		json.RawMessage(`{"tag":"a","property":{"content":"Link","url":"https://example.com/link"}}`),
		json.RawMessage(`{"tag":"button","property":{"content":"Button","behaviors":[{"type":"open_url","url":"https://example.com/button"}]}}`),
		json.RawMessage(`{"tag":"button","property":{"content":"Plain"}}`),
	}
	var got []string
	extractCardElements(elements, &got)
	want := []string{"[Link](https://example.com/link)", "[Button](https://example.com/button)", "Plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("card text = %q, want %q", got, want)
	}
}
