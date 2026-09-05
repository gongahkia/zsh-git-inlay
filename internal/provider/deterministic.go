package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/gongahkia/zsh-git-inlay/internal/candidate"
	"github.com/gongahkia/zsh-git-inlay/internal/config"
)

type Deterministic struct{}

func (Deterministic) Metadata() Metadata {
	return Metadata{Name: "deterministic", Model: "metadata-template", Quantization: "not_applicable", Runtime: "builtin", PromptVersion: config.ProviderPromptVersion, Settings: map[string]string{"staged_metadata_limit": fmt.Sprint(candidate.MaxMetadata)}}
}

func (deterministic Deterministic) Generate(ctx context.Context, request Request) (Response, error) {
	values, err := candidate.Generate(ctx, request.CWD)
	if err != nil {
		return Response{}, err
	}
	result := Response{Candidates: make([]Candidate, 0, len(values)), Metadata: deterministic.Metadata()}
	for _, value := range values {
		typeName, scope, subject, ok := splitMessage(value.Message)
		if !ok {
			return Response{}, fmt.Errorf("parse deterministic candidate")
		}
		result.Candidates = append(result.Candidates, Candidate{Type: typeName, Scope: scope, Subject: subject, EvidenceIDs: []string{"metadata:staged"}})
	}
	return result, nil
}

func splitMessage(value string) (string, string, string, bool) {
	parts := strings.SplitN(value, ": ", 2)
	if len(parts) != 2 {
		return "", "", "", false
	}
	typeName, scope := parts[0], ""
	if open := strings.IndexByte(typeName, '('); open >= 0 {
		if !strings.HasSuffix(typeName, ")") {
			return "", "", "", false
		}
		scope, typeName = typeName[open+1:len(typeName)-1], typeName[:open]
	}
	return typeName, scope, parts[1], true
}
