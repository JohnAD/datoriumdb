package adminconfig

import (
	"path/filepath"
	"strconv"

	"github.com/JohnAD/datoriumdb/internal/config"
)

func generalPath(cfg *config.Config) string {
	return filepath.Join(cfg.Dir, "__general.json")
}

func schemaPath(cfg *config.Config, collection string) string {
	return filepath.Join(cfg.Dir, collection+".schema.json")
}

func schemaVersionPath(cfg *config.Config, collection string, ver int) string {
	return filepath.Join(cfg.Dir, collection+".schema."+strconv.Itoa(ver)+".json")
}

func schemaUpdatePath(cfg *config.Config, collection string, ver int) string {
	return filepath.Join(cfg.Dir, collection+".schema."+strconv.Itoa(ver)+".update.json")
}

func searchPath(cfg *config.Config, collection, name string) string {
	return filepath.Join(cfg.Dir, collection+".search."+name+".json")
}

func searchVersionPath(cfg *config.Config, collection, name string, ver int) string {
	return filepath.Join(cfg.Dir, collection+".search."+name+"."+strconv.Itoa(ver)+".json")
}
