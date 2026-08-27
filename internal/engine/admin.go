package engine

import (
	"encoding/json"

	"github.com/JohnAD/datoriumdb/internal/adminconfig"
	"github.com/JohnAD/datoriumdb/internal/commandreq"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func (e *Engine) isEstablishmentServer() bool {
	if e.Cfg == nil || e.ServerName == "" {
		return false
	}
	return e.Cfg.General.General.EstablishmentServer == e.ServerName
}

// collectionEnsure applies declarative collection create/upgrade on the
// establishment server's config directory.
func (e *Engine) collectionEnsure(req commandreq.Request, detail map[string]any) envelope.Result {
	fields := map[string]any{"command": "collectionEnsure", "collection": req.Target}
	if req.Target == "" {
		return envelope.Fail(fields, envelope.Error{Code: "invalidRequest", Message: "target (collection) is required"})
	}
	if !e.isEstablishmentServer() {
		return envelope.Fail(fields, envelope.Error{
			Code:    "establishmentRequired",
			Message: "collectionEnsure is only accepted on the establishment server",
		})
	}

	var schemaBytes []byte
	if raw, ok := detail["schema"]; ok && raw != nil {
		b, err := json.Marshal(raw)
		if err != nil {
			return envelope.Fail(fields, envelope.Error{Code: "invalidDetail", Message: "schema must be a JSON object"})
		}
		schemaBytes = b
	}
	var upgradeRaw []byte
	if raw, ok := detail["upgrade"]; ok && raw != nil {
		b, err := json.Marshal(raw)
		if err != nil {
			return envelope.Fail(fields, envelope.Error{Code: "invalidDetail", Message: "upgrade must be a JSON object"})
		}
		upgradeRaw = b
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	store := &adminconfig.Store{ConfigDir: e.ConfigDir, DataDir: e.DataDir}
	res, errs := store.EnsureCollection(req.Target, schemaBytes, upgradeRaw)
	if len(errs) > 0 {
		return envelope.Fail(fields, errs...)
	}
	if err := e.Reload(); err != nil {
		return envelope.Fail(fields, envelope.Error{Code: "configError", Message: err.Error()})
	}
	out := map[string]any{
		"command":        "collectionEnsure",
		"collection":     req.Target,
		"changed":        res.Changed,
		"schemaVersion":  res.SchemaVersion,
		"generalVersion": res.GeneralVersion,
		"created":        res.Created,
		"upgraded":       res.Upgraded,
	}
	if res.NewVerID != "" {
		out["newVerId"] = res.NewVerID
	}
	return envelope.OK(out)
}

func (e *Engine) searchEnsure(req commandreq.Request, detail map[string]any) envelope.Result {
	fields := map[string]any{"command": "searchEnsure", "collection": req.Target, "search": req.Parameter}
	if req.Target == "" || req.Parameter == "" {
		return envelope.Fail(fields, envelope.Error{Code: "invalidRequest", Message: "target (collection) and parameter (search name) are required"})
	}
	if !e.isEstablishmentServer() {
		return envelope.Fail(fields, envelope.Error{
			Code:    "establishmentRequired",
			Message: "searchEnsure is only accepted on the establishment server",
		})
	}
	defBytes, err := json.Marshal(detail)
	if err != nil {
		return envelope.Fail(fields, envelope.Error{Code: "invalidDetail", Message: err.Error()})
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	store := &adminconfig.Store{ConfigDir: e.ConfigDir, DataDir: e.DataDir}
	res, errs := store.EnsureSearch(req.Target, req.Parameter, defBytes)
	if len(errs) > 0 {
		return envelope.Fail(fields, errs...)
	}
	if err := e.Reload(); err != nil {
		return envelope.Fail(fields, envelope.Error{Code: "configError", Message: err.Error()})
	}
	return envelope.OK(map[string]any{
		"command":        "searchEnsure",
		"collection":     req.Target,
		"search":         req.Parameter,
		"changed":        res.Changed,
		"searchVersion":  res.SearchVersion,
		"generalVersion": res.GeneralVersion,
	})
}

func (e *Engine) searchDeleteCmd(req commandreq.Request) envelope.Result {
	fields := map[string]any{"command": "searchDelete", "collection": req.Target, "search": req.Parameter}
	if req.Target == "" || req.Parameter == "" {
		return envelope.Fail(fields, envelope.Error{Code: "invalidRequest", Message: "target (collection) and parameter (search name) are required"})
	}
	if !e.isEstablishmentServer() {
		return envelope.Fail(fields, envelope.Error{
			Code:    "establishmentRequired",
			Message: "searchDelete is only accepted on the establishment server",
		})
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store := &adminconfig.Store{ConfigDir: e.ConfigDir, DataDir: e.DataDir}
	res, errs := store.DeleteSearch(req.Target, req.Parameter)
	if len(errs) > 0 {
		return envelope.Fail(fields, errs...)
	}
	if err := e.Reload(); err != nil {
		return envelope.Fail(fields, envelope.Error{Code: "configError", Message: err.Error()})
	}
	return envelope.OK(map[string]any{
		"command":        "searchDelete",
		"collection":     req.Target,
		"search":         req.Parameter,
		"changed":        res.Changed,
		"generalVersion": res.GeneralVersion,
	})
}
