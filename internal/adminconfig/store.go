package adminconfig

import (
	"bytes"
	"errors"
	"io/fs"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

// Store mutates collection and search configuration on disk.
type Store struct {
	ConfigDir string
	DataDir   string
}

type mutateFunc func(cfg *config.Config) (p *plan, result any, errs []envelope.Error)

func (s *Store) runMutation(mutate mutateFunc) (any, []envelope.Error) {
	lock, err := acquireLock(s.ConfigDir)
	if err != nil {
		var heldErr *LockHeldError
		if errors.As(err, &heldErr) {
			return nil, []envelope.Error{{Code: "configLockHeld", Message: err.Error()}}
		}
		return nil, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
	}
	defer lock.release()

	cfg, err := config.LoadUnvalidated(s.ConfigDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		return nil, []envelope.Error{{Code: "invalidJSON", Message: err.Error()}}
	}

	p, result, errs := mutate(cfg)
	if len(errs) > 0 {
		return result, errs
	}
	if p == nil {
		return result, nil
	}
	if err := p.commit(); err != nil {
		return result, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
	}
	return result, nil
}

func reindentJSON(raw []byte) ([]byte, error) {
	return config.PrettyJSONBytes(raw)
}

func jsonBytesEqual(a, b []byte) (bool, error) {
	if len(a) == 0 && len(b) == 0 {
		return true, nil
	}
	prettyA, err := reindentJSON(a)
	if err != nil {
		return false, err
	}
	prettyB, err := reindentJSON(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(prettyA, prettyB), nil
}

func ensureTrailingNewline(b []byte) []byte {
	if len(b) == 0 || b[len(b)-1] != '\n' {
		return append(b, '\n')
	}
	return b
}

func hasErrCode(errs []envelope.Error, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
