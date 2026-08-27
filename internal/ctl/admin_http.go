package ctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

type adminEndpoint struct {
	baseURL string
	token   string
}

func (c *Context) adminEndpoint(command string) (adminEndpoint, Outcome, bool) {
	baseURL := strings.TrimRight(c.EstablishmentURL, "/")
	if baseURL == "" {
		return adminEndpoint{}, ValidationFailSimple(command, "establishmentURLRequired",
			"establishment URL required: pass --establishment-url or set DATORIUMDB_ESTABLISHMENT_URL"), false
	}
	token, outcome, ok := c.resolveAdminToken(command)
	if !ok {
		return adminEndpoint{}, outcome, false
	}
	return adminEndpoint{baseURL: baseURL, token: token}, Outcome{}, true
}

func (c *Context) resolveAdminToken(command string) (string, Outcome, bool) {
	if c.AdminToken != "" {
		return c.AdminToken, Outcome{}, true
	}
	if c.SigningKeyFile == "" {
		return "", ValidationFailSimple(command, "adminTokenRequired",
			"admin authentication required: pass --admin-token or set DATORIUMDB_ADMIN_TOKEN, or provide --signing-key-file / DATORIUMDB_SIGNING_KEY_FILE to mint a token"), false
	}
	cfg, outcome, ok := loadReadOnly(c, command)
	if !ok {
		return "", outcome, false
	}
	issuer, err := auth.NewIssuerFromFile(cfg.Auth, c.SigningKeyFile)
	if err != nil {
		return "", RuntimeFailSimple(command, "filesystemError", err.Error()), false
	}
	token, _, err := issuer.IssueAdminToken("admin", 0)
	if err != nil {
		return "", RuntimeFailSimple(command, "filesystemError", err.Error()), false
	}
	return token, Outcome{}, true
}

// postAdminCommand posts an admin ensure/delete command to the establishment
// server and decodes the JSON envelope into an Outcome.
func postAdminCommand(ctx *Context, command, target, parameter string, detail any) Outcome {
	fields := map[string]any{"command": command}
	if target != "" {
		fields["target"] = target
	}
	if parameter != "" {
		fields["parameter"] = parameter
	}

	if ctx.DryRun {
		fields["establishmentURL"] = strings.TrimRight(ctx.EstablishmentURL, "/")
		fields["detail"] = detail
		return DryRunResult(fields)
	}

	ep, outcome, ok := ctx.adminEndpoint(command)
	if !ok {
		return outcome
	}

	body, err := json.Marshal(map[string]any{
		"command":   command,
		"target":    target,
		"parameter": parameter,
		"detail":    detail,
	})
	if err != nil {
		return RuntimeFail(fields, envelope.Error{Code: "invalidRequest", Message: err.Error()})
	}

	req, err := http.NewRequest(http.MethodPost, ep.baseURL+"/datoriumdb/v1/command", bytes.NewReader(body))
	if err != nil {
		return RuntimeFail(fields, envelope.Error{Code: "httpError", Message: err.Error()})
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ep.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return RuntimeFail(fields, envelope.Error{Code: "httpError", Message: err.Error()})
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return RuntimeFail(fields, envelope.Error{Code: "httpError", Message: err.Error()})
	}
	var result envelope.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return RuntimeFail(fields, envelope.Error{Code: "invalidResponse", Message: fmt.Sprintf("decode response (status %d): %v", resp.StatusCode, err)})
	}
	return outcomeFromEnvelope(result)
}

func outcomeFromEnvelope(result envelope.Result) Outcome {
	ok, _ := result["ok"].(bool)
	if ok {
		return Outcome{Result: result, Code: ExitOK}
	}
	code := ExitValidation
	if errsRaw, ok := result["errors"].([]any); ok {
		for _, item := range errsRaw {
			m, _ := item.(map[string]any)
			errCode, _ := m["code"].(string)
			if errCode == "filesystemError" || errCode == "configError" || errCode == "httpError" {
				code = ExitRuntime
				break
			}
		}
	}
	normalizeEnvelopeErrors(result)
	return Outcome{Result: result, Code: code}
}

func normalizeEnvelopeErrors(result envelope.Result) {
	errsRaw, ok := result["errors"].([]any)
	if !ok {
		return
	}
	out := make([]envelope.Error, 0, len(errsRaw))
	for _, item := range errsRaw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		e := envelope.Error{
			Code:    fmt.Sprint(m["code"]),
			Message: fmt.Sprint(m["message"]),
		}
		if p, ok := m["path"].(string); ok {
			e.Path = p
		}
		if v, ok := m["actual"]; ok {
			e.Actual = v
		}
		if v, ok := m["expected"]; ok {
			e.Expected = v
		}
		out = append(out, e)
	}
	result["errors"] = out
}

func parseJSONObject(raw []byte) (map[string]any, Outcome, bool) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, ValidationFailSimple("invalidJSON", "invalidJSON", err.Error()), false
	}
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, Outcome{}, true
}
