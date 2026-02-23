package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestArgString_RequiredValidation(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{}

	_, err := argString(req, "query", true)
	if err == nil {
		t.Fatal("expected missing required argument error")
	}

	req.Params.Arguments["query"] = 123
	_, err = argString(req, "query", true)
	if err == nil {
		t.Fatal("expected type error")
	}

	req.Params.Arguments["query"] = "  hello  "
	v, err := argString(req, "query", true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if v != "hello" {
		t.Fatalf("expected trimmed string, got %q", v)
	}
}

func TestArgFloat_Validation(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"top_k": "bad"}
	_, _, err := argFloat(req, "top_k")
	if err == nil {
		t.Fatal("expected numeric validation error")
	}
}
