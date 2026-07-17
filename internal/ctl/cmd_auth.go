package ctl

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func cmdAuthShow(ctx *Context, _ []string) Outcome {
	cfg, outcome, ok := loadReadOnly(ctx, "auth.show")
	if !ok {
		return outcome
	}
	type keySummary struct {
		Kid    string `json:"kid"`
		Alg    string `json:"alg"`
		Status string `json:"status"`
	}
	keys := make([]keySummary, 0, len(cfg.Auth.Auth.Keys))
	human := fmt.Sprintf("issuer: %s\naudience: %s\nclient token lifetime: %ds\nmachine token lifetime: %ds\nkeys:\n",
		cfg.Auth.Auth.Issuer, cfg.Auth.Auth.Audience,
		cfg.Auth.Auth.TokenLifetimeSeconds.Client, cfg.Auth.Auth.TokenLifetimeSeconds.Machine)
	for _, k := range cfg.Auth.Auth.Keys {
		keys = append(keys, keySummary{Kid: k.Kid, Alg: k.Alg, Status: k.Status})
		human += fmt.Sprintf("  %s\t%s\t%s\n", k.Kid, k.Alg, k.Status)
	}
	fields := map[string]any{
		"command":              "auth.show",
		"issuer":               cfg.Auth.Auth.Issuer,
		"audience":             cfg.Auth.Auth.Audience,
		"clientTokenLifetime":  cfg.Auth.Auth.TokenLifetimeSeconds.Client,
		"machineTokenLifetime": cfg.Auth.Auth.TokenLifetimeSeconds.Machine,
		"keys":                 keys,
	}
	return OKHuman(fields, human)
}

func cmdAuthSet(ctx *Context, args []string) Outcome {
	issuer, args, hasIssuer := extractFlag(args, "--issuer")
	audience, args, hasAudience := extractFlag(args, "--audience")
	clientLifetime, args, err := extractIntFlag(args, "--client-token-lifetime-seconds")
	if err != nil {
		return ValidationFailSimple("auth.set", "invalidArguments", err.Error())
	}
	machineLifetime, _, err := extractIntFlag(args, "--machine-token-lifetime-seconds")
	if err != nil {
		return ValidationFailSimple("auth.set", "invalidArguments", err.Error())
	}

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{}
		if hasIssuer {
			cfg.Auth.Auth.Issuer = issuer
		}
		if hasAudience {
			cfg.Auth.Auth.Audience = audience
		}
		if clientLifetime != nil {
			cfg.Auth.Auth.TokenLifetimeSeconds.Client = *clientLifetime
		}
		if machineLifetime != nil {
			cfg.Auth.Auth.TokenLifetimeSeconds.Machine = *machineLifetime
		}
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}
		plan := &Plan{}
		if err := plan.AddJSONWrite(authPath(cfg), cfg.Auth); err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "auth.set", mutate)
}

func cmdAuthKeyAdd(ctx *Context, args []string) Outcome {
	kid, args, hasKid := extractFlag(args, "--kid")
	alg, args, hasAlg := extractFlag(args, "--alg")
	keyFile, args, hasKeyFile := extractFlag(args, "--public-key-file")
	status, _, hasStatus := extractFlag(args, "--status")
	if !hasKid || !hasAlg || !hasKeyFile {
		return SimpleValidationError("auth.key.add", "invalidArguments", "usage: auth key add --kid <id> --alg <alg> --public-key-file <path> [--status active|retired]")
	}
	if !hasStatus || status == "" {
		status = "active"
	}
	if status != "active" && status != "retired" {
		return ValidationFailSimple("auth.key.add", "invalidArguments", "status must be active or retired")
	}
	rawKeyMaterial, err := os.ReadFile(keyFile)
	if err != nil {
		return ValidationFailSimple("auth.key.add", "invalidArguments", fmt.Sprintf("cannot read public key file: %v", err))
	}
	keyMaterial := strings.TrimSpace(string(rawKeyMaterial))
	if config.LooksLikePrivateKeyMaterial(keyMaterial) {
		return ValidationFail(map[string]any{"command": "auth.key.add", "kid": kid}, envelope.Error{Code: "privateKeyRejected", Message: "public-key-file looks like private key material"})
	}
	if alg == "EdDSA" {
		raw, decErr := base64.StdEncoding.DecodeString(keyMaterial)
		if decErr != nil || len(raw) != ed25519.PublicKeySize {
			return ValidationFail(map[string]any{"command": "auth.key.add", "kid": kid}, envelope.Error{Code: "invalidAuthConfig", Path: "/publicKey", Message: "publicKey must be the base64-encoded 32-byte Ed25519 public key"})
		}
	}

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{"kid": kid}
		newKey := config.AuthKey{Kid: kid, Alg: alg, Status: status, PublicKey: keyMaterial}
		if idx, exists := cfg.Auth.KeyByKid(kid); exists {
			cfg.Auth.Auth.Keys[idx] = newKey
		} else {
			cfg.Auth.Auth.Keys = append(cfg.Auth.Auth.Keys, newKey)
		}
		if cfg.Auth.ActiveKeyCount() == 0 {
			return nil, fields, []envelope.Error{{Code: "noActiveSigningKey", Message: "at least one key must remain active"}}
		}
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}
		plan := &Plan{}
		if err := plan.AddJSONWrite(authPath(cfg), cfg.Auth); err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "auth.key.add", mutate)
}

func cmdAuthKeyRetire(ctx *Context, args []string) Outcome {
	if len(args) < 1 {
		return SimpleValidationError("auth.key.retire", "invalidArguments", "usage: auth key retire <kid>")
	}
	kid := args[0]

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{"kid": kid}
		idx, exists := cfg.Auth.KeyByKid(kid)
		if !exists {
			return nil, fields, []envelope.Error{{Code: "invalidAuthConfig", Message: "unknown kid", Actual: kid}}
		}
		if cfg.Auth.Auth.Keys[idx].Status == "active" && cfg.Auth.ActiveKeyCount() <= 1 {
			return nil, fields, []envelope.Error{{Code: "noActiveSigningKey", Message: "retiring this key would leave zero active keys"}}
		}
		cfg.Auth.Auth.Keys[idx].Status = "retired"
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}
		plan := &Plan{}
		if err := plan.AddJSONWrite(authPath(cfg), cfg.Auth); err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "auth.key.retire", mutate)
}

func cmdAuthTokenIssue(ctx *Context, args []string) Outcome {
	kind, args, hasKind := extractFlag(args, "--kind")
	subject, args, _ := extractFlag(args, "--subject")
	serverName, args, _ := extractFlag(args, "--server-name")
	lifetime, _, err := extractIntFlag(args, "--lifetime-seconds")
	if err != nil {
		return ValidationFailSimple("auth.token.issue", "invalidArguments", err.Error())
	}
	if !hasKind || (kind != "client" && kind != "machine") {
		return SimpleValidationError("auth.token.issue", "invalidArguments", "--kind must be client or machine")
	}
	if kind == "machine" && serverName == "" {
		return SimpleValidationError("auth.token.issue", "invalidArguments", "--server-name is required for --kind machine")
	}

	cfg, outcome, ok := loadReadOnly(ctx, "auth.token.issue")
	if !ok {
		return outcome
	}
	if cfg.Auth.ActiveKeyCount() == 0 {
		return SimpleValidationError("auth.token.issue", "noActiveSigningKey", "no active signing key in __auth.json")
	}

	keyFile := os.Getenv("DATORIUMDB_SIGNING_KEY_FILE")
	if keyFile == "" {
		return RuntimeFailSimple("auth.token.issue", "filesystemError", "DATORIUMDB_SIGNING_KEY_FILE is not set")
	}

	issuer, err := auth.NewIssuerFromFile(cfg.Auth, keyFile)
	if err != nil {
		return RuntimeFailSimple("auth.token.issue", "filesystemError", err.Error())
	}

	var requestedLifetime time.Duration
	if lifetime != nil {
		requestedLifetime = time.Duration(*lifetime) * time.Second
	}
	if subject == "" {
		if kind == "machine" {
			subject = serverName
		} else {
			subject = "client"
		}
	}

	var token string
	var actual time.Duration
	if kind == "client" {
		token, actual, err = issuer.IssueClientToken(subject, requestedLifetime)
	} else {
		token, actual, err = issuer.IssueMachineToken(serverName, requestedLifetime)
	}
	if err != nil {
		return RuntimeFailSimple("auth.token.issue", "filesystemError", err.Error())
	}
	fields := map[string]any{
		"command":   "auth.token.issue",
		"kind":      kind,
		"kid":       issuer.Kid(),
		"token":     token,
		"expiresIn": int(actual.Seconds()),
	}
	return OKHuman(fields, token+"\n")
}
