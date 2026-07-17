package engine

import (
	"fmt"

	"github.com/JohnAD/datoriumdb/internal/accesslang"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
	"github.com/JohnAD/datoriumdb/internal/replication"
	"github.com/JohnAD/datoriumdb/internal/search"
)

// search implements ACCESS-LANGUAGE.md's `search {collection} {searchName}
// {search-parms}`: resolve the live query to an encoded bucket path exactly
// as the change-agent would for a matching document, refuse the command if
// this server does not serve that search shard, and otherwise return the
// sorted document IDs from the stored matches.json.
func (e *Engine) search(cmd accesslang.Command, detail map[string]any) envelope.Result {
	collection := cmd.Target
	searchName := cmd.Parm
	fail := func(code, message string) envelope.Result {
		return envelope.Fail(map[string]any{
			"command":    "search",
			"collection": collection,
			"search":     searchName,
		}, envelope.Error{Code: code, Message: message})
	}
	defsForCollection, ok := e.Cfg.Searches[collection]
	if !ok {
		return fail("collectionNotFound", "collection does not exist")
	}
	raw, ok := defsForCollection[searchName]
	if !ok {
		return fail("searchNotFound", "search definition does not exist")
	}
	def, err := search.ParseDefinition(raw)
	if err != nil {
		return fail("invalidSearchDefinition", err.Error())
	}
	segments, err := search.ResolveQueryPath(def, detail)
	if err != nil {
		return fail("invalidRequest", err.Error())
	}
	slot := search.ShardSlot(segments)
	if wrong := e.checkSearchRouting(slot, collection, searchName); wrong != nil {
		return *wrong
	}
	path := fsstore.SearchResultPath(e.DataDir, collection, searchName, segments)
	rf, _, err := search.LoadResultFile(path)
	if err != nil {
		return fail("filesystemError", err.Error())
	}
	ids := make([]string, 0, len(rf.Items))
	for _, it := range rf.Items {
		ids = append(ids, it.ID)
	}
	return envelope.OK(map[string]any{
		"command":    "search",
		"collection": collection,
		"search":     searchName,
		"matches":    ids,
	})
}

// checkSearchRouting refuses a search command unless this server serves
// the search result shard as either the search-shard SOT or one of its
// read/proxy members (search results are readable the same way documents
// are, per SEARCHING.md's "Search Sharding": "smart clients need to
// understand search clause rules... to know which shard slot contains the
// search result document").
func (e *Engine) checkSearchRouting(slot byte, collection, searchName string) *envelope.Result {
	if e.Cfg == nil || e.ServerName == "" {
		return nil
	}
	assignment := replication.AssignmentForSlot(e.Cfg, slot)
	if assignment.ShardSOTMember == e.ServerName || containsServer(assignment.ShardReadMember, e.ServerName) || containsServer(assignment.ProxyReadMember, e.ServerName) {
		return nil
	}
	baseURL := ""
	correctServer := assignment.ShardSOTMember
	if correctServer != "" {
		baseURL = e.Cfg.ServerBaseURL(correctServer)
	}
	res := envelope.Fail(map[string]any{
		"command":       "search",
		"collection":    collection,
		"search":        searchName,
		"shardSlot":     fmt.Sprintf("%02X", slot),
		"correctServer": correctServer,
		"baseURL":       baseURL,
		"configVersion": e.Cfg.General.General.Version,
	}, envelope.Error{
		Code:    "wrongMachine",
		Message: "This server does not serve the search result shard for this query.",
	})
	return &res
}
