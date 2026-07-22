package engine

import (
	"github.com/JohnAD/datoriumdb/internal/accesslang"
	"github.com/JohnAD/datoriumdb/internal/docjson"
	"github.com/JohnAD/ojson"
)

// canonicalCreateDocument builds on-disk document bytes from the original
// access-language detail text (OJSON, order-preserving) plus assigned
// database-owned metadata, then applies schema field order.
func canonicalCreateDocument(schemaRaw []byte, detailText, id, marker, version string) ([]byte, error) {
	doc, err := accesslang.ParseDetailValue(detailText)
	if err != nil {
		return nil, err
	}
	_, _ = doc.RemoveTry("operationId")
	if err := doc.SetTry("!", ojson.NewString(id)); err != nil {
		return nil, err
	}
	if err := doc.SetTry("$", ojson.NewString(marker)); err != nil {
		return nil, err
	}
	if err := doc.SetTry("#", ojson.NewString(version)); err != nil {
		return nil, err
	}
	return docjson.Canonicalize(schemaRaw, doc)
}
